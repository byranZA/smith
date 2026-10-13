// Package workbench owns the git a loop's execution works in: the work branch
// its tasks land on, and the task worktree and task branch each task is worked
// in, away from the operator's checkout.
package workbench

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// errNoDefaultBranch is why Setup cannot cut a work branch when origin names
// no default branch, with what fixes it.
var errNoDefaultBranch = errors.New("origin names no default branch to cut the work branch from: " +
	"give origin one (push its default branch, or point its HEAD at a branch), then check with `git ls-remote --symref origin HEAD`")

// branchRefPrefix is the namespace every branch's full ref lives in.
const branchRefPrefix = "refs/heads/"

// Runner launches a command on the machine the loop runs on. It is the system
// boundary git is reached through.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Request is what an execution's work branch is named and found by, the issue
// the execution is derived from, and the base a missing one is cut from.
type Request struct {
	Number int
	Title  string
	// Branch names the work branch exactly; empty means the one found by
	// Number, or else smith/<n>-<slug>.
	Branch string
	// Base names the branch on origin a missing work branch is cut from;
	// empty means origin's default branch.
	Base string
}

// Workbench is an execution's work branch in one repo, with the task
// worktrees its tasks are worked in.
type Workbench struct {
	git    Runner
	dir    string
	common string
	branch string
}

// Setup readies the work branch for req in the repo dir is inside: it adopts
// the branch req.Branch names, or else the one branch named smith/<n> or
// smith/<n>-<slug> for req's number, local or on origin, and otherwise cuts
// the branch WorkBranch names from the tip of req's base on origin, or of
// origin's default branch when req names none. It refuses a work branch that
// is origin's default branch or the base, more than one numbered branch, and
// one whose local and origin copies have diverged or that is checked out in a
// worktree with uncommitted changes, and never touches dir's own checkout.
func Setup(ctx context.Context, git Runner, dir string, req Request) (Workbench, error) {
	w := Workbench{git: git, dir: dir, branch: cmp.Or(req.Branch, Branch(req.Number, req.Title))}
	common, err := w.run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Workbench{}, fmt.Errorf("find the repo's git directory: %w", err)
	}
	w.common = common
	found, err := w.find(ctx, req)
	if err != nil {
		return Workbench{}, err
	}
	if found != nil {
		w.branch = found.name
	}
	if err := w.refuseDirtyCheckout(ctx); err != nil {
		return Workbench{}, err
	}
	base, err := w.base(ctx, req.Base)
	if err != nil {
		return Workbench{}, err
	}
	if found != nil {
		return w, w.adopt(ctx, *found)
	}
	if err := w.cut(ctx, base); err != nil {
		return Workbench{}, fmt.Errorf("cut work branch %s: %w", w.branch, err)
	}
	return w, nil
}

// base returns the branch a missing work branch is cut from: named, or
// origin's default branch when named is empty, which is empty itself when
// origin names none. It refuses a work branch that is origin's default branch
// or that base.
func (w Workbench) base(ctx context.Context, named string) (string, error) {
	def, err := w.defaultBranch(ctx)
	if err != nil && !errors.Is(err, errNoDefaultBranch) {
		return "", err
	}
	base := cmp.Or(named, def)
	switch w.branch {
	case def:
		return "", fmt.Errorf("work branch %s is origin's default branch: name another work branch", w.branch)
	case base:
		return "", fmt.Errorf("work branch %s is the base it is cut from: name another work branch or base", w.branch)
	}
	return base, nil
}

// WorkBranch names the work branch.
func (w Workbench) WorkBranch() string { return w.branch }

// Open returns the directory of task's worktree, creating it on a new task
// branch cut from the work branch's tip, or returning the one an earlier
// attempt left as it is.
func (w Workbench) Open(ctx context.Context, task int) (string, error) {
	dir := w.worktree(task)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if _, err := w.run(ctx, w.dir, "worktree", "add", "-b", w.taskBranch(task), dir, branchRefPrefix+w.branch); err != nil {
		return "", fmt.Errorf("add worktree %s: %w", dir, err)
	}
	return dir, nil
}

// Land fast-forwards the work branch to task's branch, in the worktree that has
// it checked out when there is one, then removes task's worktree and branch. It refuses a worktree with uncommitted changes and a
// task branch the work branch cannot fast-forward to, changing nothing.
func (w Workbench) Land(ctx context.Context, task int) error {
	dir := w.worktree(task)
	status, err := w.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read the status of worktree %s: %w", dir, err)
	}
	if status != "" {
		return fmt.Errorf("worktree %s has uncommitted changes", dir)
	}
	if err := w.fastForward(ctx, task); err != nil {
		return err
	}
	if _, err := w.run(ctx, w.dir, "worktree", "remove", dir); err != nil {
		return fmt.Errorf("remove worktree %s: %w", dir, err)
	}
	if _, err := w.run(ctx, w.dir, "branch", "-D", w.taskBranch(task)); err != nil {
		return fmt.Errorf("remove task branch %s: %w", w.taskBranch(task), err)
	}
	return nil
}

// fastForward moves the work branch to task's branch tip, guarded on the
// value it was read at, and refuses when that is not a fast-forward.
func (w Workbench) fastForward(ctx context.Context, task int) error {
	old, err := w.run(ctx, w.dir, "rev-parse", "--verify", branchRefPrefix+w.branch)
	if err != nil {
		return fmt.Errorf("read work branch %s: %w", w.branch, err)
	}
	tip, err := w.run(ctx, w.dir, "rev-parse", "--verify", branchRefPrefix+w.taskBranch(task))
	if err != nil {
		return fmt.Errorf("read task branch %s: %w", w.taskBranch(task), err)
	}
	if old == tip {
		return nil
	}
	if _, err := w.run(ctx, w.dir, "merge-base", "--is-ancestor", old, tip); err != nil {
		return fmt.Errorf("work branch %s cannot fast-forward to %s: %w", w.branch, w.taskBranch(task), err)
	}
	checkout, err := w.checkout(ctx)
	if err != nil {
		return err
	}
	if checkout != "" {
		if _, err := w.run(ctx, checkout, "merge", "--ff-only", "--quiet", branchRefPrefix+w.taskBranch(task)); err != nil {
			return fmt.Errorf("fast-forward work branch %s in %s: %w", w.branch, checkout, err)
		}
		return nil
	}
	if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+w.branch, tip, old); err != nil {
		return fmt.Errorf("fast-forward work branch %s: %w", w.branch, err)
	}
	return nil
}

// refuseDirtyCheckout refuses a work branch checked out in a worktree with
// uncommitted changes, which landing a task would have to move.
func (w Workbench) refuseDirtyCheckout(ctx context.Context) error {
	checkout, err := w.checkout(ctx)
	if err != nil || checkout == "" {
		return err
	}
	status, err := w.run(ctx, checkout, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read the status of %s: %w", checkout, err)
	}
	if status != "" {
		return fmt.Errorf("work branch %s is checked out in %s, which has uncommitted changes: commit or stash them, then run again", w.branch, checkout)
	}
	return nil
}

// checkout returns the directory of the worktree that has the work branch
// checked out, or empty when none has.
func (w Workbench) checkout(ctx context.Context) (string, error) {
	out, err := w.run(ctx, w.dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("list worktrees: %w", err)
	}
	var dir string
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			dir = path
		}
		if line == "branch "+branchRefPrefix+w.branch {
			return dir, nil
		}
	}
	return "", nil
}

// cut creates the work branch at the tip of base on origin, fetched for the
// purpose. An empty base is origin naming no default branch.
func (w Workbench) cut(ctx context.Context, base string) error {
	if base == "" {
		return errNoDefaultBranch
	}
	tip, err := w.fetch(ctx, base)
	if err != nil {
		return err
	}
	if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+w.branch, tip, ""); err != nil {
		return fmt.Errorf("create it at origin's %s: %w", base, err)
	}
	return nil
}

// match is a branch that is a work branch by name, and where it exists.
type match struct {
	name            string
	local, onOrigin bool
}

// find returns the one branch, local or on origin, that req's work branch can
// be: the one req.Branch names or, when it names none, a work branch of req's
// number by name. It returns nil when there is none, and more than one is an
// error naming each.
func (w Workbench) find(ctx context.Context, req Request) (*match, error) {
	prefix, owns := "smith/", func(name string) bool { return belongs(name, req.Number) }
	if req.Branch != "" {
		prefix, owns = req.Branch, func(name string) bool { return name == req.Branch }
	}
	local, err := w.run(ctx, w.dir, "for-each-ref", "--format=%(refname)", branchRefPrefix+prefix)
	if err != nil {
		return nil, fmt.Errorf("list local branches under %s: %w", prefix, err)
	}
	remote, err := w.run(ctx, w.dir, "ls-remote", "origin", branchRefPrefix+prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("list origin's branches under %s: %w", prefix, err)
	}
	found := map[string]*match{}
	note := func(name string) *match {
		if found[name] == nil {
			found[name] = &match{name: name}
		}
		return found[name]
	}
	for _, name := range owned(strings.Lines(local), owns) {
		note(name).local = true
	}
	for _, name := range owned(lsRemoteRefs(remote), owns) {
		note(name).onOrigin = true
	}
	names := slices.Sorted(maps.Keys(found))
	switch len(names) {
	case 0:
		return nil, nil
	case 1:
		return found[names[0]], nil
	default:
		return nil, fmt.Errorf("more than one branch is a work branch of #%d: %s", req.Number, strings.Join(names, ", "))
	}
}

// owned returns the names of the branches among refs that owns accepts.
func owned(refs iter.Seq[string], owns func(name string) bool) []string {
	var mine []string
	for ref := range refs {
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), branchRefPrefix)
		if ok && owns(name) {
			mine = append(mine, name)
		}
	}
	return mine
}

// lsRemoteRefs yields the ref of each line of git ls-remote's output.
func lsRemoteRefs(out string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for line := range strings.Lines(out) {
			_, ref, _ := strings.Cut(line, "\t")
			if !yield(ref) {
				return
			}
		}
	}
}

// adopt makes m the work branch: a local copy is kept, one only on origin is
// created locally from origin's copy, and local and origin copies that have
// diverged are refused.
func (w Workbench) adopt(ctx context.Context, m match) error {
	if !m.onOrigin {
		return nil
	}
	theirs, err := w.fetch(ctx, m.name)
	if err != nil {
		return err
	}
	if !m.local {
		if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+m.name, theirs, ""); err != nil {
			return fmt.Errorf("create work branch %s from origin's copy: %w", m.name, err)
		}
		return nil
	}
	ours, err := w.run(ctx, w.dir, "rev-parse", "--verify", branchRefPrefix+m.name)
	if err != nil {
		return fmt.Errorf("read work branch %s: %w", m.name, err)
	}
	if w.isAncestor(ctx, ours, theirs) || w.isAncestor(ctx, theirs, ours) {
		return nil
	}
	return fmt.Errorf("work branch %s has diverged from origin's copy: reconcile the two, then run again", m.name)
}

// isAncestor reports whether commit a is an ancestor of, or the same as,
// commit b.
func (w Workbench) isAncestor(ctx context.Context, a, b string) bool {
	_, err := w.run(ctx, w.dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// fetch fetches branch from origin and returns its tip there.
func (w Workbench) fetch(ctx context.Context, branch string) (string, error) {
	if _, err := w.run(ctx, w.dir, "fetch", "--quiet", "origin", branchRefPrefix+branch); err != nil {
		return "", fmt.Errorf("fetch origin's %s: %w", branch, err)
	}
	tip, err := w.run(ctx, w.dir, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("read origin's %s: %w", branch, err)
	}
	return tip, nil
}

// defaultBranch asks origin, over git alone, which branch its HEAD names. A
// clone's own origin/HEAD is not consulted: a bare clone never sets it.
func (w Workbench) defaultBranch(ctx context.Context) (string, error) {
	out, err := w.run(ctx, w.dir, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("find origin's default branch: %w", err)
	}
	for line := range strings.Lines(out) {
		ref, ok := strings.CutPrefix(strings.TrimSpace(line), "ref: "+branchRefPrefix)
		if name, isHead := strings.CutSuffix(ref, "\tHEAD"); ok && isHead {
			return name, nil
		}
	}
	return "", errNoDefaultBranch
}

// worktree returns where task's worktree lives, under the repo's git common
// directory so no checkout is dirtied.
func (w Workbench) worktree(task int) string {
	return filepath.Join(w.common, "smith", "worktrees", strconv.Itoa(task))
}

// taskBranch names task's branch, in a namespace of smith's own beside the
// work branch it is cut from.
func (w Workbench) taskBranch(task int) string {
	return "smith/tasks/" + w.branch + "/" + strconv.Itoa(task)
}

// run runs git in dir with args and returns its trimmed output, or an error
// carrying what git said on stderr.
func (w Workbench) run(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := w.git.Run(ctx, "git", append([]string{"-C", dir}, args...), nil, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
