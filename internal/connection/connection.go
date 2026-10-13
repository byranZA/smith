// Package connection is the one exec boundary smith uses to reach a box: a thin
// wrapper over the ssh and scp binaries on PATH. It runs remote commands with
// streamed output, delivers values over stdin rather than argv, and copies
// local files to the box.
package connection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrConnect indicates ssh could not establish a connection to the box, as
// distinct from a remote command that ran and failed. Callers branch on this
// with errors.Is to report a connect failure.
var ErrConnect = errors.New("could not connect to box")

// ErrAuthRefused is the connect failure where the box answered but refused the
// key. It wraps ErrConnect, so it still counts as a connect failure.
var ErrAuthRefused = fmt.Errorf("%w: the box answered but refused the key", ErrConnect)

// ErrUnreachable is the connect failure where the box did not answer at all.
// It wraps ErrConnect, so it still counts as a connect failure.
var ErrUnreachable = fmt.Errorf("%w: the box did not answer", ErrConnect)

// authHint and unreachableHint tell the operator what to check for each kind
// of connect failure.
const (
	authHint        = "check the key is loaded in ssh-agent or set by a Host entry in ~/.ssh/config (a passphrase key must be agent-loaded), and that the login is right"
	unreachableHint = "check the address, that sshd is listening on port 22, and the provider's firewall"
)

// authMarkers and unreachableMarkers are the ssh stderr fragments that mark
// each kind of connect failure.
var (
	authMarkers        = []string{"Permission denied", "Too many authentication failures"}
	unreachableMarkers = []string{"Connection timed out", "Connection refused", "No route to host", "Could not resolve hostname"}
)

// stderrTailLimit bounds how much of ssh's stderr is kept for classifying a
// connect failure, since a streamed remote command may write without end.
const stderrTailLimit = 4096

// connectExitCode is ssh's conventional exit status when the connection itself
// fails (as opposed to the remote command's own exit code).
const connectExitCode = 255

// defaultSSHOptions harden the non-interactive bootstrap-in path: never prompt
// for a password (key auth only), auto-accept a fresh box's host key, and time
// out rather than hang when the box is unreachable.
var defaultSSHOptions = []string{
	"-o", "BatchMode=yes",
	"-o", "StrictHostKeyChecking=accept-new",
	"-o", "ConnectTimeout=10",
}

// Exec abstracts launching a local process (ssh or scp). It is injected so
// tests can fake the boundary without a real box.
type Exec interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error. A non-nil error from a process that ran
	// exposes its exit status via an ExitCode() int method.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// SSH reaches a single box over ssh/scp. The target is the ssh destination —
// "login@host" during the bootstrap-in path, "smith@host" thereafter.
type SSH struct {
	target string
	exec   Exec
}

// New returns an SSH bound to target that launches processes through exec.
// Pass System() as exec for the real ssh/scp binaries.
func New(target string, exec Exec) *SSH {
	return &SSH{target: target, exec: exec}
}

// Run executes remoteCmd on the box, streaming its stdout and stderr to the
// given writers as it goes. It returns an error wrapping ErrConnect when the
// connection could not be established.
func (s *SSH) Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error {
	return s.run(ctx, remoteCmd, nil, stdout, stderr)
}

// RunWithInput executes remoteCmd on the box with stdin delivered from the
// given reader, so a value (such as a secret) reaches the box over stdin and
// never appears in argv. Output is streamed as in Run.
func (s *SSH) RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	return s.run(ctx, remoteCmd, stdin, stdout, stderr)
}

// run executes remoteCmd over ssh with the given stdin, classifying a failure
// to connect as ErrConnect while still streaming stderr to the caller.
func (s *SSH) run(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	args := append(append([]string{}, defaultSSHOptions...), s.target, remoteCmd)
	var tail tailBuffer
	if err := s.exec.Run(ctx, "ssh", args, stdin, stdout, teeTo(stderr, &tail)); err != nil {
		return s.classify("ssh", err, string(tail.buf))
	}
	return nil
}

// TerminalArgs renders the ssh argv that runs remoteCmd on target with a TTY
// requested. It is for a caller that replaces its own process with ssh — the
// terminal then talks to the remote program directly, so resizes and signals
// reach it rather than being copied to it — which is why the argv is returned
// rather than run, and why it takes no Exec to run it through.
func TerminalArgs(target, remoteCmd string) []string {
	return append(append([]string{}, defaultSSHOptions...), "-t", target, remoteCmd)
}

// Copy sends the local file at localPath to remotePath on the box with scp. A
// copy the box refused carries scp's own error text, such as a full disk.
func (s *SSH) Copy(ctx context.Context, localPath, remotePath string) error {
	args := append(append([]string{}, defaultSSHOptions...), localPath, s.target+":"+remotePath)
	var stderr bytes.Buffer
	if err := s.exec.Run(ctx, "scp", args, nil, io.Discard, &stderr); err != nil {
		err = s.classify("scp", err, stderr.String())
		if text := strings.TrimSpace(stderr.String()); text != "" && !errors.Is(err, ErrConnect) {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

// ShellArg single-quotes s so it interpolates as one literal argument in the
// remote shell commands passed to Run and RunWithInput.
func ShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// classify maps a process error to ErrConnect when it carries ssh's connect
// exit status, otherwise wraps it with context. A connect failure is narrowed
// by stderr to ErrAuthRefused or ErrUnreachable with a hint, or else carries
// ssh's last stderr line.
func (s *SSH) classify(tool string, err error, stderr string) error {
	var coder interface{ ExitCode() int }
	if !errors.As(err, &coder) || coder.ExitCode() != connectExitCode {
		return fmt.Errorf("%s %s: %w", tool, s.target, err)
	}
	switch {
	case containsAny(stderr, authMarkers):
		return fmt.Errorf("%s %s: %w; %s", tool, s.target, ErrAuthRefused, authHint)
	case containsAny(stderr, unreachableMarkers):
		return fmt.Errorf("%s %s: %w; %s", tool, s.target, ErrUnreachable, unreachableHint)
	}
	if line := lastLine(stderr); line != "" {
		return fmt.Errorf("%s %s: %w: %s", tool, s.target, ErrConnect, line)
	}
	return fmt.Errorf("%s %s: %w", tool, s.target, ErrConnect)
}

// containsAny reports whether text contains any of the markers.
func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// lastLine returns the last non-blank line of text, trimmed.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// tailBuffer is an io.Writer that keeps only the last stderrTailLimit bytes
// written to it.
type tailBuffer struct {
	buf []byte
}

// Write appends p, dropping the oldest bytes beyond stderrTailLimit. It never
// fails.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTailLimit; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

// teeTo writes to both w and tail, or to tail alone when w is nil.
func teeTo(w io.Writer, tail *tailBuffer) io.Writer {
	if w == nil {
		return tail
	}
	return io.MultiWriter(w, tail)
}
