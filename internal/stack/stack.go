// Package stack runs a repo's stack: the services its repo file declares with
// an up and a down command. It brings the stack up for a worktree before an
// agent works there and takes it down when the run ends. It is handed the
// Commander its commands run through and looks nothing up itself.
package stack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/byranZA/smith/internal/repofile"
)

// The steps a stack command runs as, which name it in a failure.
const (
	// Up is the step that brings the stack up.
	Up = "stack up"
	// Down is the step that takes the stack down.
	Down = "stack down"
)

// DownTimeout bounds a down command, which has no setting.
const DownTimeout = 5 * time.Minute

// Commander runs a command in a worktree the way the prepare command runs. It
// is the system boundary the stack reaches its commands through.
type Commander interface {
	// Exec runs command in the worktree at dir as step, streaming its output
	// to out, and returns its failure.
	Exec(ctx context.Context, dir, step, command string, out io.Writer) error
}

// Stack brings one repo's stack up for a worktree and takes it down. It
// remembers the commands it ran up with, so it takes down what it started.
// It is for one run at a time and is not safe for concurrent use.
type Stack struct {
	// Command runs the up and down commands.
	Command Commander
	// State is the state home the stack lease lives in, Key names the repo's
	// lease in it and Branch is the run's work branch, which the lease
	// records. An empty State takes no lease.
	State, Key, Branch string

	running *running
	lease   *lease
}

// running is a stack that was brought up: where, and the commands that go
// with the up that ran.
type running struct {
	dir, up, down string
}

// Ensure brings the stack the repo file in dir declares up, with its output
// streamed to out. A stack left running for another worktree, or by other
// commands, is taken down first. A repo file with no stack takes down any
// stack that is running and runs nothing more. A failed up is a
// *prepare.StepError, and its stack stays recorded so Release takes it down.
func (s *Stack) Ensure(ctx context.Context, dir string, out io.Writer) error {
	file, err := repofile.Load(repofile.At(dir))
	if err != nil {
		return fmt.Errorf("read the stack: %w", err)
	}
	declared := running{dir: dir, up: file.Stack.Up, down: file.Stack.Down}
	if s.running != nil && (!file.Stack.Declared() || *s.running != declared) {
		if err := s.Release(ctx, out); err != nil {
			return err
		}
	}
	if !file.Stack.Declared() {
		return nil
	}
	if err := s.hold(ctx, declared, out); err != nil {
		return err
	}
	s.running = &declared
	if err := s.Command.Exec(ctx, dir, Up, file.Stack.Up, out); err != nil {
		return fmt.Errorf("bring up the stack in %s: %w", dir, err)
	}
	return nil
}

// hold takes the stack lease, when there is none held and the stack has a
// state home, and first takes down any stack its record shows a dead run left, then records
// declared in it before the up that runs it.
func (s *Stack) hold(ctx context.Context, declared running, out io.Writer) error {
	if s.State == "" {
		return nil
	}
	if s.lease == nil {
		l, err := acquire(s.State, s.Key)
		if err != nil {
			return err
		}
		if _, err := s.recoverHeld(ctx, l, out); err != nil {
			return errors.Join(err, l.close())
		}
		s.lease = l
	}
	return s.lease.write(Record{Dir: declared.dir, Branch: s.Branch, Up: declared.up, Down: declared.down, Started: time.Now().UTC()})
}

// Release takes down the stack Ensure brought up, once, in the worktree it
// came up in with the down recorded for it, streaming its output to out. It
// runs to the end though ctx is cancelled, and is stopped at DownTimeout.
// The lease's record is deleted only when down succeeded, and its lock is
// freed either way. With no stack running it does nothing. The stack counts
// as taken down whether or not down succeeded.
func (s *Stack) Release(ctx context.Context, out io.Writer) error {
	if s.running == nil {
		return nil
	}
	r := *s.running
	s.running = nil
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DownTimeout)
	defer cancel()
	err := s.Command.Exec(ctx, r.dir, Down, r.down, out)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("take down the stack in %s: timed out after %s: %w", r.dir, DownTimeout, err)
		} else {
			err = fmt.Errorf("take down the stack in %s: %w", r.dir, err)
		}
	}
	if err == nil && s.lease != nil {
		err = s.lease.drop()
	}
	if s.lease != nil {
		err = errors.Join(err, s.lease.close())
		s.lease = nil
	}
	return err
}
