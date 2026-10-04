package loop

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/byranZA/smith/internal/agent"
)

// Process is the Launcher that starts a real agent process in the directory
// smith runs from, with smith's environment plus the command's own.
type Process struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Launch runs cmd to completion, streaming its output, and stops it when ctx
// is cancelled. The agent reads no input.
func (p Process) Launch(ctx context.Context, cmd agent.Command) error {
	proc := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	proc.Env = append(os.Environ(), cmd.Env...)
	proc.Stdout = p.Stdout
	proc.Stderr = p.Stderr
	if err := proc.Run(); err != nil {
		return fmt.Errorf("run %s: %w", cmd.Name, err)
	}
	return nil
}
