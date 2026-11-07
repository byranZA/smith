package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/misetrust"
	"github.com/byranZA/smith/internal/tailbuf"
)

// Process is the Launcher that starts a real agent process in the command's
// directory, with smith's environment plus the command's own. Only an
// attached command reads Stdin.
type Process struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Mise is the mise executable the agent runs under, in its worktree and
	// trusting the configs there. Empty runs the agent directly.
	Mise string
}

// ErrKilled is what Launch's error is when the agent was killed by SIGKILL,
// most likely by the kernel for running out of memory.
var ErrKilled = errors.New("agent killed by SIGKILL")

// ErrNotStarted is what Launch's error is when the agent's process could not
// be started at all, such as when its binary is not on PATH.
var ErrNotStarted = errors.New("agent not started")

// stopDelay bounds how long Launch waits, once an agent has exited or been
// killed, for its output to close.
const stopDelay = 3 * time.Second

// Launch runs cmd to completion, streaming its output (a HoldStderr command's
// stderr only if the run fails), and stops it when ctx is
// cancelled: an unattended agent reads no input and is stopped with the
// processes it started, while an attached one reads Stdin and keeps smith's
// process group, and with it the terminal. An agent killed by SIGKILL, or
// exiting 137 as a shell does for a child so killed, fails with ErrKilled, and
// one whose process cannot be started fails with ErrNotStarted.
func (p Process) Launch(ctx context.Context, cmd agent.Command) error {
	argv := p.argv(cmd)
	proc := exec.CommandContext(ctx, argv[0], argv[1:]...)
	proc.Env = append(os.Environ(), cmd.Env...)
	proc.Dir = cmd.Dir
	proc.Stdout = p.Stdout
	proc.Stderr = p.Stderr
	var held *tailbuf.Buffer
	if cmd.HoldStderr && !cmd.Attached {
		held = &tailbuf.Buffer{Limit: HeldTailLimit}
		proc.Stderr = held
	}
	proc.WaitDelay = stopDelay
	if cmd.Attached {
		proc.Stdin = p.Stdin
	} else {
		stopTogether(proc)
	}
	if err := proc.Start(); err != nil {
		return fmt.Errorf("run %s: %w: %w", cmd.Name, ErrNotStarted, err)
	}
	err := proc.Wait()
	if exitedLeavingOutputOpen(err) {
		return nil
	}
	if killed(err) {
		err = fmt.Errorf("run %s: %w: %w", cmd.Name, ErrKilled, err)
	} else if err != nil {
		err = fmt.Errorf("run %s: %w", cmd.Name, err)
	}
	if held != nil && err != nil {
		if rerr := p.showHeld(held); rerr != nil {
			return errors.Join(err, rerr)
		}
	}
	return err
}

// exitedLeavingOutputOpen reports whether err is from an agent that exited
// successfully while a background process it started kept its output open past
// stopDelay, which is still a successful run.
func exitedLeavingOutputOpen(err error) bool {
	return errors.Is(err, exec.ErrWaitDelay)
}

// HeldTailLimit bounds how much of a held-back agent's stderr is kept to show
// if the run fails.
const HeldTailLimit = 64 << 10

// showHeld writes the stderr held back from a failed run to Stderr, after a
// note when earlier output was cut.
func (p Process) showHeld(held *tailbuf.Buffer) error {
	if held.Dropped() {
		if _, err := fmt.Fprintf(p.Stderr, "smith: earlier agent output was cut; showing the last %d KiB\n", HeldTailLimit>>10); err != nil {
			return fmt.Errorf("write held agent output: %w", err)
		}
	}
	if _, err := p.Stderr.Write(held.Lines()); err != nil {
		return fmt.Errorf("write held agent output: %w", err)
	}
	return nil
}

// argv is what Launch runs for cmd: the agent under mise exec in its
// directory, trusting the configs there, or the agent itself with no Mise.
func (p Process) argv(cmd agent.Command) []string {
	argv := append([]string{cmd.Name}, cmd.Args...)
	if p.Mise == "" {
		return argv
	}
	return misetrust.Command(p.Mise, cmd.Dir, append([]string{"--cd", cmd.Dir, "exec", "--"}, argv...)...)
}
