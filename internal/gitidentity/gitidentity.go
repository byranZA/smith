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
	"errors"
	"fmt"
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
		UserName:  starterValue(ctx, git, "user.name"),
		UserEmail: starterValue(ctx, git, "user.email"),
	}
}

// starterValue is key from the global git config trimmed for the starter, or
// empty when git does not answer with one for any reason.
func starterValue(ctx context.Context, git Runner, key string) string {
	value, err := Value(ctx, git, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// unsetStatus is the status `git config --get` exits with for a key no config
// file sets.
const unsetStatus = 1

// Value returns key from the global git config exactly as git holds it, or
// empty when no config file sets it. Files the global config includes are
// followed, and git answers with the last declaration it reads, so this is the
// value git itself commits with; scoping to --global alone would skip an
// identity declared in an included file.
//
// Any other way git fails to answer is an error carrying git's own diagnostic,
// and a cancelled ctx is reported as such rather than read as an unset key.
func Value(ctx context.Context, git Runner, key string) (string, error) {
	var stdout, stderr bytes.Buffer
	err := git.Run(ctx, "git", []string{"config", "--global", "--includes", "--get", key}, nil, &stdout, &stderr)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", fmt.Errorf("read %s from the global git config: %w", key, ctxErr)
	}
	if err == nil {
		return strings.TrimSuffix(stdout.String(), "\n"), nil
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() == unsetStatus {
		return "", nil
	}
	if diagnostic := strings.TrimSpace(stderr.String()); diagnostic != "" {
		return "", fmt.Errorf("read %s from the global git config: %w: %s", key, err, diagnostic)
	}
	return "", fmt.Errorf("read %s from the global git config: %w", key, err)
}
