// Package connection is the one exec boundary smith uses to reach a box: a thin
// wrapper over the system ssh and scp binaries. It runs a remote command with
// streamed output, delivers a value to a remote command over stdin (never as a
// command-line argument, so secrets do not leak into argv or process listings),
// and copies a local file to the box with scp.
//
// It assumes ssh and scp are on PATH (Windows 10 1809+ and 11 ship OpenSSH).
package connection

import (
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

func (s *SSH) run(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	args := append(append([]string{}, defaultSSHOptions...), s.target, remoteCmd)
	if err := s.exec.Run(ctx, "ssh", args, stdin, stdout, stderr); err != nil {
		return s.classify("ssh", err)
	}
	return nil
}

// Copy sends the local file at localPath to remotePath on the box with scp.
func (s *SSH) Copy(ctx context.Context, localPath, remotePath string) error {
	args := append(append([]string{}, defaultSSHOptions...), localPath, s.target+":"+remotePath)
	if err := s.exec.Run(ctx, "scp", args, nil, io.Discard, io.Discard); err != nil {
		return s.classify("scp", err)
	}
	return nil
}

// ShellArg single-quotes s so it interpolates as one argument in a remote shell
// command, keeping the value out of any shell-special interpretation. It is the
// escaping primitive callers use when building the command strings passed to
// Run and RunWithInput.
func ShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// classify maps a process error to ErrConnect when it carries ssh's connect
// exit status, otherwise wraps it with context.
func (s *SSH) classify(tool string, err error) error {
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) && coder.ExitCode() == connectExitCode {
		return fmt.Errorf("%s %s: %w", tool, s.target, ErrConnect)
	}
	return fmt.Errorf("%s %s: %w", tool, s.target, err)
}
