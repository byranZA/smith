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
	"io/fs"
	"iter"
	"maps"
	"os"
	"os/exec"
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
	lag    Lag
}

// Lag is how far an adopted work branch was behind the tip of its base on
// origin when Setup adopted it. Its zero value is no check run: the work
// branch was freshly cut, or there was no base to compare it with.
type Lag struct {
	// Base names the branch on origin the work branch was compared with.
	Base string
	// Commits counts the base's commits the work branch lacks.
	Commits int
	// Err is why the comparison could not run; nil when it ran.
	Err error
}

// Setup readies the work branch for req in the repo dir is inside: it adopts
// the branch req.Branch names, or else the one branch named smith/<n> or
// smith/<n>-<slug> for req's number, local or on origin, comparing it with the
// tip of its base on origin for Lag, and otherwise cuts the branch WorkBranch
// names from the tip of req's base on origin, or of origin's default branch
// when req names none. It refuses a work branch that is origin's default branch
// or the base, more than one numbered branch, and one whose local and origin
// copies have diverged or that is checked out in a worktree with uncommitted
// changes, and never touches dir's own checkout.
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
	if _, err := w.cleanCheckout(ctx); err != nil {
		return Workbench{}, err
	}
	base, err := w.base(ctx, req.Base)
	if err != nil {
		return Workbench{}, err
	}
	if found != nil {
		if err := w.adopt(ctx, *found); err != nil {
			return Workbench{}, err
		}
		w.lag = w.behind(ctx, base)
		return w, nil
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

// Lag reports how far the work branch was behind its base when Setup adopted
// it.
func (w Workbench) Lag() Lag { return w.lag }

// Open returns the directory of task's worktree: the one that already has
// task's branch checked out, as an earlier attempt left it; else a landed
// task's recycled worktree, switched to a new task branch cut from the work
// branch's tip; else a new worktree on that branch. It refuses, leaving it in
// place, anything at a new worktree's path.
func (w Workbench) Open(ctx context.Context, task int) (string, error) {
	trees, err := w.worktrees(ctx)
	if err != nil {
		return "", err
	}
	if dir, ok := w.holder(trees, task); ok {
		return dir, nil
	}
	if i := slices.IndexFunc(trees, w.recycled); i >= 0 {
		dir := trees[i].dir
		if _, err := w.run(ctx, dir, "switch", "--quiet", "-c", w.TaskBranch(task), branchRefPrefix+w.branch); err != nil {
			return "", fmt.Errorf("switch worktree %s to task branch %s: %w", dir, w.TaskBranch(task), err)
		}
		return dir, nil
	}
	return w.add(ctx, task, trees)
}

// recycled reports whether t is a worktree of the execution's that a landed
// task left with no branch checked out, for the next task to reuse.
func (w Workbench) recycled(t tree) bool {
	return t.ref == "" && filepath.Dir(t.dir) == w.executionDir()
}

// add creates task's worktree on a new task branch cut from the work branch's
// tip, refusing anything already at its path, which trees, the worktrees git
// registers, says whether it is this repo's.
func (w Workbench) add(ctx context.Context, task int, trees []tree) (string, error) {
	dir := w.worktree(task)
	_, err := os.Lstat(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("look for worktree %s: %w", dir, err)
	}
	if err == nil {
		return "", w.occupied(task, dir, trees)
	}
	if _, err := w.run(ctx, w.dir, "worktree", "add", "-b", w.TaskBranch(task), dir, branchRefPrefix+w.branch); err != nil {
		return "", fmt.Errorf("add worktree %s: %w", dir, err)
	}
	return dir, nil
}

// occupied is why task's new worktree cannot be added at dir, where trees,
// the worktrees git registers, already holds something that is not task's.
func (w Workbench) occupied(task int, dir string, trees []tree) error {
	i := slices.IndexFunc(trees, func(t tree) bool { return t.dir == dir })
	if i < 0 {
		return fmt.Errorf("%s is not a worktree of this repo, so cannot hold task branch %s: move it aside, then run again", dir, w.TaskBranch(task))
	}
	return fmt.Errorf("worktree %s has %s checked out, not task branch %s: another execution left it, so finish or remove it there, then run again",
		dir, cmp.Or(strings.TrimPrefix(trees[i].ref, branchRefPrefix), "a detached HEAD"), w.TaskBranch(task))
}

// Land fast-forwards the work branch to task's branch, in the worktree that has
// it checked out when there is one, then removes task's branch, keeping its
// worktree, detached at the landed commit, for the next task to recycle. It
// refuses, changing nothing, a task worktree or work-branch checkout with
// uncommitted changes and a task branch the work branch cannot fast-forward to.
func (w Workbench) Land(ctx context.Context, task int) error {
	dir, err := w.taskTree(ctx, task)
	if err != nil {
		return err
	}
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
	if _, err := w.run(ctx, dir, "switch", "--quiet", "--detach"); err != nil {
		return fmt.Errorf("detach worktree %s from task branch %s: %w", dir, w.TaskBranch(task), err)
	}
	if _, err := w.run(ctx, w.dir, "branch", "-D", w.TaskBranch(task)); err != nil {
		return fmt.Errorf("remove task branch %s: %w", w.TaskBranch(task), err)
	}
	return nil
}

// taskTree returns the directory of the worktree that has task's branch
// checked out, and is an error when none has.
func (w Workbench) taskTree(ctx context.Context, task int) (string, error) {
	trees, err := w.worktrees(ctx)
	if err != nil {
		return "", err
	}
	dir, ok := w.holder(trees, task)
	if !ok {
		return "", fmt.Errorf("no worktree has task branch %s checked out", w.TaskBranch(task))
	}
	return dir, nil
}

// holder returns the directory of the worktree among trees that has task's
// branch checked out, and reports whether one has.
func (w Workbench) holder(trees []tree, task int) (string, bool) {
	i := slices.IndexFunc(trees, func(t tree) bool { return t.ref == branchRefPrefix+w.TaskBranch(task) })
	if i < 0 {
		return "", false
	}
	return trees[i].dir, true
}

// Ahead counts the commits task's branch has that the work branch does not,
// and is an error when either branch cannot be read.
func (w Workbench) Ahead(ctx context.Context, task int) (int, error) {
	out, err := w.run(ctx, w.dir, "rev-list", "--count", branchRefPrefix+w.branch+".."+branchRefPrefix+w.TaskBranch(task))
	if err != nil {
		return 0, fmt.Errorf("count the commits of task branch %s: %w", w.TaskBranch(task), err)
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("count the commits of task branch %s: %w", w.TaskBranch(task), err)
	}
	return n, nil
}

// fastForward moves the work branch to task's branch tip, guarded on the
// value it was read at, and refuses when that is not a fast-forward.
func (w Workbench) fastForward(ctx context.Context, task int) error {
	old, err := w.run(ctx, w.dir, "rev-parse", "--verify", branchRefPrefix+w.branch)
	if err != nil {
		return fmt.Errorf("read work branch %s: %w", w.branch, err)
	}
	tip, err := w.run(ctx, w.dir, "rev-parse", "--verify", branchRefPrefix+w.TaskBranch(task))
	if err != nil {
		return fmt.Errorf("read task branch %s: %w", w.TaskBranch(task), err)
	}
	if old == tip {
		return nil
	}
	if _, err := w.run(ctx, w.dir, "merge-base", "--is-ancestor", old, tip); err != nil {
		return fmt.Errorf("work branch %s cannot fast-forward to %s: %w", w.branch, w.TaskBranch(task), err)
	}
	checkout, err := w.cleanCheckout(ctx)
	if err != nil {
		return err
	}
	if checkout != "" {
		if _, err := w.run(ctx, checkout, "merge", "--ff-only", "--quiet", branchRefPrefix+w.TaskBranch(task)); err != nil {
			return fmt.Errorf("fast-forward work branch %s in %s: %w", w.branch, checkout, err)
		}
		return nil
	}
	if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+w.branch, tip, old); err != nil {
		return fmt.Errorf("fast-forward work branch %s: %w", w.branch, err)
	}
	return nil
}

// cleanCheckout returns the directory of the worktree that has the work branch
// checked out, or empty when none has, and refuses one with uncommitted
// changes, which landing a task would have to move.
func (w Workbench) cleanCheckout(ctx context.Context) (string, error) {
	checkout, err := w.checkout(ctx)
	if err != nil || checkout == "" {
		return "", err
	}
	status, err := w.run(ctx, checkout, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("read the status of %s: %w", checkout, err)
	}
	if status != "" {
		return "", fmt.Errorf("work branch %s is checked out in %s, which has uncommitted changes: commit or stash them, then run again", w.branch, checkout)
	}
	return checkout, nil
}

// checkout returns the directory of the worktree that has the work branch
// checked out, or empty when none has.
func (w Workbench) checkout(ctx context.Context) (string, error) {
	trees, err := w.worktrees(ctx)
	if err != nil {
		return "", err
	}
	for _, t := range trees {
		if t.ref == branchRefPrefix+w.branch {
			return t.dir, nil
		}
	}
	return "", nil
}

// tree is a worktree git registers for the repo, and the full ref of the
// branch it has checked out, empty when none is.
type tree struct {
	dir, ref string
}

// worktrees returns every worktree git registers for the repo.
func (w Workbench) worktrees(ctx context.Context) ([]tree, error) {
	out, err := w.run(ctx, w.dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	var trees []tree
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if dir, ok := strings.CutPrefix(line, "worktree "); ok {
			trees = append(trees, tree{dir: dir})
		}
		if ref, ok := strings.CutPrefix(line, "branch "); ok && len(trees) > 0 {
			trees[len(trees)-1].ref = ref
		}
	}
	return trees, nil
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
	for _, pair := range [][2]string{{ours, theirs}, {theirs, ours}} {
		behind, err := w.isAncestor(ctx, pair[0], pair[1])
		if err != nil {
			return fmt.Errorf("compare work branch %s with origin's copy: %w", m.name, err)
		}
		if behind {
			return nil
		}
	}
	return fmt.Errorf("work branch %s has diverged from origin's copy: reconcile the two, then run again", m.name)
}

// behind compares the work branch with the tip of base on origin, fetched for
// the purpose, without moving it. An empty base is no check run.
func (w Workbench) behind(ctx context.Context, base string) Lag {
	if base == "" {
		return Lag{}
	}
	lag := Lag{Base: base}
	tip, err := w.fetch(ctx, base)
	if err != nil {
		lag.Err = err
		return lag
	}
	out, err := w.run(ctx, w.dir, "rev-list", "--count", branchRefPrefix+w.branch+".."+tip)
	if err != nil {
		lag.Err = fmt.Errorf("count origin's %s commits work branch %s lacks: %w", base, w.branch, err)
		return lag
	}
	if lag.Commits, err = strconv.Atoi(out); err != nil {
		lag.Err = fmt.Errorf("count origin's %s commits work branch %s lacks: %w", base, w.branch, err)
	}
	return lag
}

// isAncestor reports whether commit a is an ancestor of, or the same as,
// commit b. Only git's exit status 1 is a no; any other failure is an error.
func (w Workbench) isAncestor(ctx context.Context, a, b string) (bool, error) {
	_, err := w.run(ctx, w.dir, "merge-base", "--is-ancestor", a, b)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	default:
		return false, err
	}
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

// executionDir returns the directory the execution's task worktrees live in,
// named for the work branch and under the repo's git common directory so no
// checkout is dirtied.
func (w Workbench) executionDir() string {
	return filepath.Join(w.common, "smith", "worktrees", pathSegment(w.branch))
}

// worktree returns where a new worktree cut for task lives, in the
// execution's directory.
func (w Workbench) worktree(task int) string {
	return filepath.Join(w.executionDir(), strconv.Itoa(task))
}

// pathSegment reduces s to one path segment by replacing every character
// outside [A-Za-z0-9._-] with a hyphen.
func pathSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
}

// TaskBranch names task's branch, in a namespace of smith's own beside the
// work branch it is cut from.
func (w Workbench) TaskBranch(task int) string {
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
