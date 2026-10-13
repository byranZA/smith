// Package workbench owns the git a loop's execution works in: the work branch
// its tasks land on, and the task worktree and task branch each task is worked
// in, away from the operator's checkout.
package workbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Request is what an execution's work branch is named and found by: the issue
// the execution is derived from.
type Request struct {
	Number int
	Title  string
}

// Workbench is an execution's work branch in one repo, with the task
// worktrees its tasks are worked in.
type Workbench struct {
	git    Runner
	dir    string
	common string
	branch string
}

// Setup readies the work branch for req in the repo dir is inside: it keeps
// the branch Branch names when it exists, and otherwise cuts it from the tip
// of origin's default branch. It never touches dir's own checkout.
func Setup(ctx context.Context, git Runner, dir string, req Request) (Workbench, error) {
	w := Workbench{git: git, dir: dir, branch: Branch(req.Number, req.Title)}
	common, err := w.run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Workbench{}, fmt.Errorf("find the repo's git directory: %w", err)
	}
	w.common = common
	exists, err := w.exists(ctx, w.branch)
	if err != nil {
		return Workbench{}, err
	}
	if exists {
		return w, nil
	}
	if err := w.cut(ctx); err != nil {
		return Workbench{}, fmt.Errorf("cut work branch %s: %w", w.branch, err)
	}
	return w, nil
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

// Land fast-forwards the work branch to task's branch, then removes task's
// worktree and branch. It refuses a worktree with uncommitted changes and a
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
	if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+w.branch, tip, old); err != nil {
		return fmt.Errorf("fast-forward work branch %s: %w", w.branch, err)
	}
	return nil
}

// cut creates the work branch at the tip of origin's default branch, fetched
// for the purpose.
func (w Workbench) cut(ctx context.Context) error {
	base, err := w.defaultBranch(ctx)
	if err != nil {
		return err
	}
	if _, err := w.run(ctx, w.dir, "fetch", "--quiet", "origin", branchRefPrefix+base); err != nil {
		return fmt.Errorf("fetch origin's %s: %w", base, err)
	}
	tip, err := w.run(ctx, w.dir, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("read origin's %s: %w", base, err)
	}
	if _, err := w.run(ctx, w.dir, "update-ref", branchRefPrefix+w.branch, tip, ""); err != nil {
		return fmt.Errorf("create it at origin's %s: %w", base, err)
	}
	return nil
}

// exists reports whether branch exists locally.
func (w Workbench) exists(ctx context.Context, branch string) (bool, error) {
	out, err := w.run(ctx, w.dir, "for-each-ref", "--format=%(refname)", branchRefPrefix+branch)
	if err != nil {
		return false, fmt.Errorf("look for branch %s: %w", branch, err)
	}
	return out != "", nil
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
