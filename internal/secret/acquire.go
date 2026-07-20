package secret

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNoReference is returned when no reference was given and there is no
// interactive terminal to prompt on: nothing to resolve and nothing to ask, so
// acquisition fails fast rather than hanging.
var ErrNoReference = errors.New("no secret reference given and no interactive terminal to prompt")

// Terminal is the interactive boundary Acquire falls back to when the reference
// is omitted. It reports whether a terminal is attached and reads a secret from
// it without echoing the typed characters.
type Terminal interface {
	// Interactive reports whether a terminal is attached to prompt on.
	Interactive() bool
	// ReadSecret writes prompt and reads one line from the terminal without
	// echoing what is typed.
	ReadSecret(prompt string) (string, error)
}

// Acquire obtains the secret value. When ref is non-empty it resolves the
// reference (see Resolve). When ref is empty it prompts term without echo using
// prompt as the label; on a non-interactive terminal it fails fast with
// ErrNoReference. The prompted value is whitespace-trimmed like a resolved one.
func Acquire(ref, prompt string, term Terminal) (string, error) {
	if ref != "" {
		return Resolve(ref)
	}
	if term == nil || !term.Interactive() {
		return "", ErrNoReference
	}
	value, err := term.ReadSecret(prompt)
	if err != nil {
		return "", fmt.Errorf("prompt for secret: %w", err)
	}
	return strings.TrimSpace(value), nil
}

// StdTerminal is the real Terminal, backed by the process's standard input and
// standard error. It prompts on stderr so a piped stdout stays free of the
// prompt, and reads the secret from stdin with echo disabled.
type StdTerminal struct {
	in  *os.File
	out io.Writer
}

// NewStdTerminal returns a StdTerminal reading from os.Stdin and prompting on
// os.Stderr.
func NewStdTerminal() *StdTerminal {
	return &StdTerminal{in: os.Stdin, out: os.Stderr}
}

// Interactive reports whether stdin is a terminal.
func (t *StdTerminal) Interactive() bool {
	return term.IsTerminal(int(t.in.Fd()))
}

// ReadSecret writes prompt to stderr, reads a line from stdin without echo, and
// returns it. The trailing newline the user's Enter would normally echo is
// emitted explicitly so following output starts on a fresh line.
func (t *StdTerminal) ReadSecret(prompt string) (string, error) {
	if _, err := io.WriteString(t.out, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	value, err := term.ReadPassword(int(t.in.Fd()))
	if err != nil {
		return "", fmt.Errorf("read from terminal: %w", err)
	}
	if _, err := io.WriteString(t.out, "\n"); err != nil {
		return "", fmt.Errorf("write newline: %w", err)
	}
	return string(value), nil
}
