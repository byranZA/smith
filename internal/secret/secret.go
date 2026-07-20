// Package secret resolves a secret reference into its value without the secret
// ever touching a command line.
//
// Config carries a reference, never the value: a scheme:arg string such as
// env:VAR or file:/path. The reference is split on the first colon only, so a
// Windows path (file:C:\keys\ts) survives, and the resolved value is
// whitespace-trimmed. A bare literal with no scheme is a hard error — env: is
// the documented escape hatch — so a raw secret is never accepted on argv.
//
// When the reference is omitted, Acquire prompts an interactive terminal
// without echo; with no terminal to prompt and no reference to resolve it fails
// fast. The scheme:arg slot is the durable primitive for future backends (op:,
// keychain:, pass:); this package lands env: and file:.
package secret

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrBareLiteral is returned when a reference carries no scheme: a bare secret
// value on argv is refused so it never leaks into shell history or process
// listings. Use env: as the escape hatch.
var ErrBareLiteral = errors.New("secret reference has no scheme (bare literals are refused; use env: or file:)")

// ErrUnknownScheme is returned for a reference whose scheme is not one this
// package knows how to resolve.
var ErrUnknownScheme = errors.New("unknown secret reference scheme")

// ErrEmptyArg is returned when a scheme's argument is empty, e.g. env: with no
// variable name or file: with no path.
var ErrEmptyArg = errors.New("secret reference has an empty argument")

// ErrNotFound is returned when a referenced source holds no value, e.g. an
// unset environment variable.
var ErrNotFound = errors.New("secret reference resolved to nothing")

// Resolve turns a scheme:arg reference into its secret value. It splits on the
// first colon only, so file:C:\keys\ts keeps its Windows drive letter, dispatches
// on the scheme (env: reads an environment variable, file: reads a file), and
// whitespace-trims the result. A reference with no scheme is ErrBareLiteral.
func Resolve(ref string) (string, error) {
	scheme, arg, ok := strings.Cut(ref, ":")
	if !ok {
		return "", fmt.Errorf("resolve %q: %w", ref, ErrBareLiteral)
	}
	if arg == "" {
		return "", fmt.Errorf("resolve %q: %w", ref, ErrEmptyArg)
	}

	switch scheme {
	case "env":
		return resolveEnv(arg)
	case "file":
		return resolveFile(arg)
	default:
		return "", fmt.Errorf("resolve %q: %q: %w", ref, scheme, ErrUnknownScheme)
	}
}

// resolveEnv reads the named environment variable. An unset variable is
// ErrNotFound; note env: is a scheme, not a SMITH_* per-flag fallback, so only
// the exact name is consulted.
func resolveEnv(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q: %w", name, ErrNotFound)
	}
	return strings.TrimSpace(value), nil
}

// resolveFile reads the file at path. A missing file wraps the os error so
// callers can errors.Is it against os.ErrNotExist.
func resolveFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
