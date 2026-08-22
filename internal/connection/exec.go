package connection

import (
	"context"
	"io"
	"os/exec"
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
