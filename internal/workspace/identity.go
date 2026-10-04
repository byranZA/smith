package workspace

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/gitidentity"
	"github.com/byranZA/smith/internal/staging"
)

// gitKey is one identity field as git config names it, with the value the
// operator declared for it.
type gitKey struct {
	name, value string
}

// declaredKeys are the git config keys the identity declares, in the order
// git config lists them. A field nobody declared has no key here, so it is
// neither set nor unset.
func declaredKeys(id staging.Identity) []gitKey {
	var keys []gitKey
	if id.UserName != "" {
		keys = append(keys, gitKey{"user.name", id.UserName})
	}
	if id.UserEmail != "" {
		keys = append(keys, gitKey{"user.email", id.UserEmail})
	}
	return keys
}

// setIdentity sets each declared identity key that differs from the one git
// commits with for the smith user, and reports what it set and what already
// held.
//
// It sets keys through `git config --global` rather than writing ~/.gitconfig,
// because smith does not own that file: anything else in it survives. A file
// that config includes after its own keys outranks them, so each write is read
// back the way git commits: a key the include still overrides fails the step
// rather than reporting an identity git does not use.
func setIdentity(ctx context.Context, run Runner, id staging.Identity, progress io.Writer) (string, error) {
	var set, held []string
	for _, k := range declaredKeys(id) {
		if gitidentity.Value(ctx, run, k.name) == k.value {
			held = append(held, k.name)
			continue
		}
		args := []string{"config", "--global", k.name, k.value}
		if err := run.Run(ctx, "git", args, nil, progress, progress); err != nil {
			return "", fmt.Errorf("set %s in the smith user's global git config: %w", k.name, err)
		}
		if got := gitidentity.Value(ctx, run, k.name); got != k.value {
			return "", fmt.Errorf("set %s in the smith user's global git config, but git still commits with %q: a file that config includes overrides it", k.name, got)
		}
		set = append(set, k.name)
	}
	return identitySummary(set, held), nil
}

// identitySummary renders what the identity step set and what already held,
// in the words the operator is told it.
func identitySummary(set, held []string) string {
	var parts []string
	if len(set) > 0 {
		parts = append(parts, "set "+strings.Join(set, ", "))
	}
	if len(held) > 0 {
		parts = append(parts, strings.Join(held, ", ")+" already set")
	}
	return strings.Join(parts, "; ")
}
