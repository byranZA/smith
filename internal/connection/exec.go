package connection

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// SystemExec runs real local processes via os/exec. It is the production Exec
// used to launch the system ssh and scp binaries.
type SystemExec struct{}

// System returns the SystemExec that launches the real ssh/scp binaries.
func System() SystemExec { return SystemExec{} }

// Run launches name with args through os/exec, wiring the provided
// stdin/stdout/stderr straight through so output streams live.
//
// The subprocess inherits smith's own environment. That is how a provider CLI
// reaches its credential: smith checks only that a required variable is
// present and never reads its value, and the command picks it up itself.
func (SystemExec) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return err //nolint:wrapcheck // exit error is classified by the caller
	}
	return nil
}

// Exec replaces smith's own process with name run with args, searching PATH
// for the binary the way a shell does and carrying smith's environment over.
//
// It is how smith hands the terminal to an interactive program: with no smith
// process left in the middle, the terminal talks to that program directly, so
// window resizes and signals reach it rather than being copied to it. It
// returns only when the replacement did not happen, because on success there
// is no longer a caller to return to.
func (SystemExec) Exec(name string, args []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("find %s on PATH: %w", name, err)
	}
	if err := syscall.Exec(path, append([]string{name}, args...), os.Environ()); err != nil {
		return fmt.Errorf("replace smith with %s: %w", name, err)
	}
	return nil
}
