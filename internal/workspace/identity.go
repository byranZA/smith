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

// setIdentity writes each declared key that differs from the value git commits
// with into file, the smith user's ~/.gitconfig, and reports what it set and
// what already held. A failed read, or a read-back that git still answers with
// another value, fails the step.
func setIdentity(ctx context.Context, run Runner, id staging.Identity, file string, progress io.Writer) (string, error) {
	var set, held []string
	for _, k := range declaredKeys(id) {
		current, err := gitidentity.Value(ctx, run, k.name)
		if err != nil {
			return "", fmt.Errorf("check %s in the smith user's global git config: %w", k.name, err)
		}
		if current == k.value {
			held = append(held, k.name)
			continue
		}
		args := []string{"config", "--file", file, k.name, k.value}
		if err := run.Run(ctx, "git", args, nil, progress, progress); err != nil {
			return "", fmt.Errorf("set %s in %s: %w", k.name, file, err)
		}
		got, err := gitidentity.Value(ctx, run, k.name)
		if err != nil {
			return "", fmt.Errorf("read back %s from the smith user's global git config: %w", k.name, err)
		}
		if got != k.value {
			return "", fmt.Errorf("set %s in %s, but git still commits with %q: a file that config includes overrides it", k.name, file, got)
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
