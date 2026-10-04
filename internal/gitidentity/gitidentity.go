// Package gitidentity reads the git identity a user commits as from their
// global git config: smith init carries the operator's into the starter
// preferences, and the workspace stage checks the smith user's against the
// declared one.
//
// Only the global git config is read. A repository's local identity belongs to
// that repository, and init reads nothing from wherever it happens to be run.
package gitidentity

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
)

// Runner launches a command on the operator's machine. It is the system
// boundary git is reached through, injected so a test can stand in for git.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Global returns the user.name and user.email set in the operator's global
// git config, each left empty when it is not set, and trimmed of the stray
// whitespace a hand-edited config can carry.
//
// Finding no identity is an ordinary result, not a failure: git exits non-zero
// for an unset key, and a machine without git has no identity to offer. Every
// way git can fail to answer therefore reads as the value being unset, because
// the identity only fills in a starter the operator can still edit by hand.
func Global(ctx context.Context, git Runner) blueprint.Git {
	return blueprint.Git{
		UserName:  strings.TrimSpace(Value(ctx, git, "user.name")),
		UserEmail: strings.TrimSpace(Value(ctx, git, "user.email")),
	}
}

// Value returns key from the global git config exactly as git holds it, or
// empty when git does not answer with one. Files the global config includes
// are followed, and git answers with the last declaration it reads, so this is
// the value git itself commits with; scoping to --global alone would skip an
// identity declared in an included file.
func Value(ctx context.Context, git Runner, key string) string {
	var stdout bytes.Buffer
	if err := git.Run(ctx, "git", []string{"config", "--global", "--includes", "--get", key}, nil, &stdout, io.Discard); err != nil {
		return ""
	}
	return strings.TrimSuffix(stdout.String(), "\n")
}
