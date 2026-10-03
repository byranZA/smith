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

// Run launches name with args, streaming the given stdin/stdout/stderr live.
// The subprocess inherits smith's environment, which is how a provider CLI
// reaches a credential smith never reads.
func (SystemExec) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

// Exec replaces smith's process with name run with args from PATH, handing the
// terminal straight to that program. It returns only when the replacement did
// not happen.
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
