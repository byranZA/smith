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
// commits with for the smith user, writing it into file, and reports what it
// set and what already held.
//
// It writes through `git config --file` naming ~/.gitconfig rather than
// `--global`, which git sends to ~/.config/git/config when only that exists: a
// placement there would then replace the identity on every run. It sets keys
// one by one rather than writing the file, because smith does not own it:
// anything else in it survives. A file that config includes after its own keys
// outranks them, so each write is read back the way git commits: a key the
// include still overrides fails the step rather than reporting an identity git
// does not use. A read that fails other than for an unset key fails the step
// before anything is written.
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
