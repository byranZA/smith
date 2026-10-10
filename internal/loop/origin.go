package loop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errNoDefaultBranch is why the loop pushes nothing when origin names no
// default branch, with what fixes it.
var errNoDefaultBranch = errors.New("origin names no default branch, so smith cannot tell whether this branch is it: " +
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

// Origin is the Pusher that pushes the branch checked out in Dir to the
// origin remote with git.
type Origin struct {
	Git Runner
	Dir string
}

// Push pushes the current branch to origin under its own name, setting that
// as its upstream and never forcing, or skips it when it is origin's default
// branch. The refspec names both ends in full, so neither a tag of the same
// name nor push config can send the branch anywhere else.
func (o Origin) Push(ctx context.Context) (Pushed, error) {
	branch, err := o.currentBranch(ctx)
	if err != nil {
		return Pushed{}, err
	}
	def, err := o.defaultBranch(ctx)
	if err != nil {
		return Pushed{}, err
	}
	if branch == def {
		return Pushed{Branch: branch, Skipped: true}, nil
	}
	ref := branchRefPrefix + branch
	if _, err := o.git(ctx, "push", "--set-upstream", "origin", ref+":"+ref); err != nil {
		return Pushed{}, fmt.Errorf("push %s to origin: %w", branch, err)
	}
	return Pushed{Branch: branch}, nil
}

// currentBranch returns the name of the branch checked out in Dir, read from
// HEAD's full ref so a tag of the same name cannot disguise it.
func (o Origin) currentBranch(ctx context.Context) (string, error) {
	ref, err := o.git(ctx, "symbolic-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("find the current branch: %w", err)
	}
	branch, ok := strings.CutPrefix(ref, branchRefPrefix)
	if !ok || branch == "" {
		return "", fmt.Errorf("find the current branch: HEAD names %s, which is not a branch", ref)
	}
	return branch, nil
}

// defaultBranch asks origin, over git alone, which branch its HEAD names. A
// clone's own origin/HEAD is not consulted: a bare clone never sets it.
func (o Origin) defaultBranch(ctx context.Context) (string, error) {
	out, err := o.git(ctx, "ls-remote", "--symref", "origin", "HEAD")
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

// git runs git in Dir with args and returns its trimmed output, or an error
// carrying what git said on stderr.
func (o Origin) git(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := o.Git.Run(ctx, "git", append([]string{"-C", o.Dir}, args...), nil, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
