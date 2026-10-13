package loop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// branchRefPrefix is the namespace every branch's full ref lives in.
const branchRefPrefix = "refs/heads/"

// Runner launches a command on the machine the loop runs on. It is the system
// boundary git is reached through.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Origin is the Pusher that pushes a branch of the repo Dir is in to the
// origin remote with git.
type Origin struct {
	Git Runner
	Dir string
}

// Push pushes branch to origin under its own name, setting that as its
// upstream and never forcing. The refspec names both ends in full, so neither
// a tag of the same name nor push config can send the branch anywhere else.
func (o Origin) Push(ctx context.Context, branch string) error {
	ref := branchRefPrefix + branch
	if _, err := o.git(ctx, "push", "--set-upstream", "origin", ref+":"+ref); err != nil {
		return fmt.Errorf("push %s to origin: %w", branch, err)
	}
	return nil
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
