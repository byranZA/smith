package secret

import (
	"bufio"
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

// StdTerminal is the real Terminal, backed by the process's standard streams.
// It prompts on stderr so a piped stdout stays free of the prompt, reads a
// secret from stdin with echo disabled, and is the one place in smith that asks
// the operating system whether a stream is a terminal.
type StdTerminal struct {
	in      *os.File
	display *os.File
	out     io.Writer
	// isTerminal is the operating-system boundary, a field so a test drives
	// the stream combinations without a pty to hand.
	isTerminal func(fd uintptr) bool
}

// NewStdTerminal returns a StdTerminal reading from os.Stdin and prompting on
// os.Stderr, judging attendance by os.Stdin and os.Stdout.
func NewStdTerminal() *StdTerminal {
	return &StdTerminal{
		in:         os.Stdin,
		display:    os.Stdout,
		out:        os.Stderr,
		isTerminal: func(fd uintptr) bool { return term.IsTerminal(int(fd)) },
	}
}

// Interactive reports whether stdin is a terminal, which is all a secret
// prompt needs: it is typed on stdin and asked for on stderr, so a redirected
// stdout is still a run a human can hand a key to.
func (t *StdTerminal) Interactive() bool {
	return t.isTerminal(t.in.Fd())
}

// Attended reports whether a human is at this terminal to be asked a question
// whose answer changes what smith does. It is stricter than Interactive: stdin
// must be a terminal, so a `< /dev/null` invocation never hangs waiting for an
// answer nobody will type, and stdout must be one too, because a run whose
// output is being collected is a scripted run that should be told what to do
// rather than asked.
func (t *StdTerminal) Attended() bool {
	return t.isTerminal(t.in.Fd()) && t.isTerminal(t.display.Fd())
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

// ReadLine writes prompt to stderr and reads one echoed line from stdin,
// returned without its line ending. Unlike ReadSecret what is typed stays
// visible: the answer to a question smith asks is not a secret, and seeing it
// is how the operator knows what they answered. Input that ends without a
// newline comes back as it stands, so a stdin that closes answers with an empty
// line rather than an error.
func (t *StdTerminal) ReadLine(prompt string) (string, error) {
	if _, err := io.WriteString(t.out, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	line, err := bufio.NewReader(t.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read from terminal: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
