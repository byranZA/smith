// Package prepare makes a worktree ready to work in, in the foreground, before
// anyone works there: it installs the toolchain the repo's own mise config
// pins, then runs the prepare command the worktree's own repo file declares.
// It is handed the process runner, the mise executable it drives and the
// directory whose mise configs it trusts, and looks nothing up itself, so a
// test fakes mise at the command boundary.
package prepare

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/misetrust"
	"github.com/byranZA/smith/internal/repofile"
)

// Toolchain is the step that installs what the worktree's mise config pins.
const Toolchain = "toolchain"

// RepoCommand is the step that runs the prepare command the worktree's repo
// file declares.
const RepoCommand = "prepare"

// inDir is the shell script that runs its second argument as a command in the
// directory its first argument names, which is how the prepare command runs in
// the worktree when there is no mise to change into it.
const inDir = `cd -- "$1" && exec sh -c "$2"`

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
	// Mise is the mise executable the toolchain is installed with and the
	// prepare command runs under. Empty means there is no mise: the toolchain
	// step is skipped and the prepare command runs directly.
	Mise string
	// Trust is the smith-owned directory whose mise configs, at or below it,
	// mise trusts while preparing. Empty trusts the worktree being prepared.
	Trust string
}

// StepError reports the preparation step that failed and the status it exited
// with.
type StepError struct {
	// Step names the step that failed, such as Toolchain.
	Step string
	// Status is the exit status the step's process answered with, or -1 when
	// the process could not run at all.
	Status int
	// Command is the command the step ran, as the operator knows it.
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

// Prepare installs the toolchain the worktree at dir pins, then runs the
// prepare command the repo file in dir declares, if any, in dir under that
// toolchain, streaming the output of both to out as it is printed. A failed
// step is a *StepError naming the step and its exit status, and a failed
// toolchain stops before the prepare command.
func (p Preparer) Prepare(ctx context.Context, dir string, out io.Writer) error {
	if p.Mise != "" {
		install := []string{p.Mise, "--cd", dir, "install"}
		if err := p.step(ctx, Toolchain, strings.Join(install, " "), out, p.trusting(dir, install)); err != nil {
			return err
		}
	}
	file, err := repofile.Load(repofile.At(dir))
	if err != nil {
		return fmt.Errorf("read the prepare command: %w", err)
	}
	if file.Prepare == "" {
		return nil
	}
	run := []string{"sh", "-c", inDir, "sh", dir, file.Prepare}
	if p.Mise != "" {
		run = p.trusting(dir, []string{p.Mise, "--cd", dir, "exec", "--", "sh", "-c", file.Prepare})
	}
	return p.step(ctx, RepoCommand, file.Prepare, out, run)
}

// trusting is the mise argv run with mise trusting the configs in Trust, or
// in the worktree at dir when Trust is empty.
func (p Preparer) trusting(dir string, argv []string) []string {
	return misetrust.Command(argv[0], cmp.Or(p.Trust, dir), argv[1:]...)
}

// step runs one preparation step's argv, streaming both its output streams to
// out, and answers a failure as a *StepError naming step and the command it
// ran as shown.
func (p Preparer) step(ctx context.Context, step, shown string, out io.Writer, argv []string) error {
	err := p.Command.Run(ctx, argv[0], argv[1:], nil, out, out)
	if err == nil {
		return nil
	}
	failed := &StepError{Step: step, Status: -1, Command: shown, Err: err}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		failed.Status = exit.ExitCode()
	}
	return failed
}
