package loop

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/byranZA/smith/internal/agent"
)

// Process is the Launcher that starts a real agent process in the directory
// smith runs from, with smith's environment plus the command's own.
type Process struct {
	Stdout io.Writer
	Stderr io.Writer
}

// stopDelay bounds how long Launch waits, once an interrupted agent is
// killed, for its output to close.
const stopDelay = 3 * time.Second

// Launch runs cmd to completion, streaming its output, and stops it and the
// processes it started when ctx is cancelled. The agent reads no input.
func (p Process) Launch(ctx context.Context, cmd agent.Command) error {
	proc := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	proc.Env = append(os.Environ(), cmd.Env...)
	proc.Stdout = p.Stdout
	proc.Stderr = p.Stderr
	proc.WaitDelay = stopDelay
	stopTogether(proc)
	if err := proc.Run(); err != nil {
		return fmt.Errorf("run %s: %w", cmd.Name, err)
	}
	return nil
}
