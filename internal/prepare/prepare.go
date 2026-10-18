// Package prepare makes a worktree ready to work in: it installs the toolchain
// the repo's own mise config pins, in the foreground, before anyone works
// there. It is handed the process runner and the mise executable it drives
// and looks nothing up itself, so a test fakes mise at the command boundary.
package prepare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Toolchain is the step that installs what the worktree's mise config pins.
const Toolchain = "toolchain"

// Runner launches a local command. It is the system boundary preparation
// reaches mise through.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Preparer prepares worktrees by driving mise through a runner.
type Preparer struct {
	// Command launches the processes preparation runs.
	Command Runner
	// Mise is the mise executable the toolchain is installed with.
	Mise string
}

// StepError reports the preparation step that failed and the status it exited
// with.
type StepError struct {
	// Step names the step that failed, such as Toolchain.
	Step string
	// Status is the exit status the step's process answered with, or -1 when
	// the process could not run at all.
	Status int
	// Command is the command line the step ran.
	Command string
	// Err is the process error.
	Err error
}

// Error implements error.
func (e *StepError) Error() string {
	if e.Status < 0 {
		return fmt.Sprintf("the %s step could not run: %s: %v", e.Step, e.Command, e.Err)
	}
	return fmt.Sprintf("the %s step exited with status %d: %s", e.Step, e.Status, e.Command)
}

// Unwrap returns the process error.
func (e *StepError) Unwrap() error { return e.Err }

// Prepare installs the toolchain the worktree at dir pins, streaming mise's
// output to out as it is printed. A failure is a *StepError naming the step
// and its exit status.
func (p Preparer) Prepare(ctx context.Context, dir string, out io.Writer) error {
	return p.step(ctx, Toolchain, out, p.Mise, "--cd", dir, "install")
}

// step runs one preparation step's command, streaming both its output streams
// to out, and answers a failure as a *StepError naming step.
func (p Preparer) step(ctx context.Context, step string, out io.Writer, name string, args ...string) error {
	err := p.Command.Run(ctx, name, args, nil, out, out)
	if err == nil {
		return nil
	}
	failed := &StepError{Step: step, Status: -1, Command: strings.Join(append([]string{name}, args...), " "), Err: err}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		failed.Status = exit.ExitCode()
	}
	return failed
}
