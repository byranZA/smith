// Package stack runs a repo's stack: the services its repo file declares with
// an up and a down command. It brings the stack up for a worktree and takes it
// down when the run ends.
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

// Stack brings one repo's stack up for a worktree and takes it down with the
// commands it ran up with. It is for one run at a time.
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

// Ensure brings up the stack the repo file in dir declares, first taking down
// one running for another worktree or commands. A failed up is a
// *prepare.StepError whose stack stays recorded for Release.
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

// hold takes the stack lease if the stack has a state home and holds none,
// recovering a dead run's stack first, and records declared in it.
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

// Release takes down the stack Ensure brought up, once, even if ctx is
// cancelled, bounded by DownTimeout. It deletes the lease record only when down
// succeeded; with no stack running it does nothing.
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
