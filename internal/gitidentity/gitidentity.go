// Package gitidentity reads the git identity the operator already commits as
// on the operator's machine, so smith init can carry it into the starter
// preferences rather than leaving the operator to type it again.
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
// git config, each left empty when it is not set.
//
// Finding no identity is an ordinary result, not a failure: git exits non-zero
// for an unset key, and a machine without git has no identity to offer. Every
// way git can fail to answer therefore reads as the value being unset, because
// the identity only fills in a starter the operator can still edit by hand.
func Global(ctx context.Context, git Runner) blueprint.Git {
	return blueprint.Git{
		UserName:  globalValue(ctx, git, "user.name"),
		UserEmail: globalValue(ctx, git, "user.email"),
	}
}

// globalValue returns key from the global git config, or empty when git does
// not answer with one. Files the global config includes are followed, since
// git itself commits with an identity declared there; scoping to --global
// alone would skip them.
func globalValue(ctx context.Context, git Runner, key string) string {
	var stdout bytes.Buffer
	if err := git.Run(ctx, "git", []string{"config", "--global", "--includes", "--get", key}, nil, &stdout, io.Discard); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}
