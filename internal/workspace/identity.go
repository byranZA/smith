package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

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

// setIdentity sets each declared identity key that differs in the smith
// user's global git config, and reports what it set and what already held.
//
// It sets keys through `git config --global` rather than writing ~/.gitconfig,
// because smith does not own that file: anything else in it survives.
func setIdentity(ctx context.Context, run Runner, id staging.Identity, progress io.Writer) (string, error) {
	var set, held []string
	for _, k := range declaredKeys(id) {
		if globalConfig(ctx, run, k.name) == k.value {
			held = append(held, k.name)
			continue
		}
		args := []string{"config", "--global", k.name, k.value}
		if err := run.Run(ctx, "git", args, nil, progress, progress); err != nil {
			return "", fmt.Errorf("set %s in the smith user's global git config: %w", k.name, err)
		}
		set = append(set, k.name)
	}
	return identitySummary(set, held), nil
}

// globalConfig is the value the smith user's global git config holds for key.
// git exits non-zero for a key it does not hold, so a failed read is an unset
// key rather than an error: the write that follows reports a config git
// cannot touch.
func globalConfig(ctx context.Context, run Runner, key string) string {
	var out bytes.Buffer
	if err := run.Run(ctx, "git", []string{"config", "--global", "--get", key}, nil, &out, io.Discard); err != nil {
		return ""
	}
	return strings.TrimSuffix(out.String(), "\n")
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
