package loop

import (
	"context"
	"fmt"
	"io"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/tracker"
)

// Tracker is where the loop reads a spec and its tasks.
type Tracker interface {
	// Spec reads spec number and its tasks as they stand now.
	Spec(ctx context.Context, number int) (tracker.Spec, error)
}

// Launcher is the one seam through which the loop starts a process.
type Launcher interface {
	// Launch runs cmd to completion, streaming its output to the operator.
	Launch(ctx context.Context, cmd agent.Command) error
}

// Loop works one spec's tasks with a coding agent, one task per agent run.
// It never commits, pushes, switches branch or looks at the working tree:
// that is the agent's work, asked for through the prompt, so a failed
// attempt's changes are still there for the next.
type Loop struct {
	Tracker  Tracker
	Launcher Launcher
	Agent    agent.Adapter
	Prompt   Prompt
	Limits   Limits
	// Progress is where the loop names each task as it hands it out.
	Progress io.Writer
}

// Limits bound an unattended run so it can be left alone.
type Limits struct {
	// MaxIterations caps the agent runs the whole loop makes.
	MaxIterations int
	// MaxAttempts caps the agent runs one task gets before it is skipped.
	MaxAttempts int
}

// DefaultLimits are the limits of a run that sets none: ten agent runs in
// all, and two attempts at each task.
func DefaultLimits() Limits { return Limits{MaxIterations: 10, MaxAttempts: 2} }

// Outcome is how a run ended: what remained of the spec when it stopped,
// with the tasks it skipped among it, and whether the iteration cap stopped it.
type Outcome struct {
	Remaining Remaining
	Capped    bool
}

// Complete reports whether the run ended with every task of the spec closed.
func (o Outcome) Complete() bool { return o.Remaining.Complete() }

// Run works spec until no task is available for an agent, re-reading the
// tracker before every selection so a task filed mid-run is seen. A task the
// agent leaves open is handed out again until it has had MaxAttempts runs,
// then skipped; the run stops once it has made MaxIterations agent runs.
// Cancelling ctx stops the running agent and starts no further task.
func (l Loop) Run(ctx context.Context, spec int) (Outcome, error) {
	attempts := map[int]int{}
	skipped := map[int]bool{}
	runs := 0
	for {
		current, err := l.Tracker.Spec(ctx, spec)
		if err != nil {
			return Outcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
		}
		remaining := Survey(current).Skip(skipped)
		next, ok := remaining.Next()
		if !ok {
			return Outcome{Remaining: remaining}, nil
		}
		if runs >= l.Limits.MaxIterations {
			return Outcome{Remaining: remaining, Capped: true}, nil
		}
		if err := l.hand(ctx, next, spec); err != nil {
			return Outcome{}, err
		}
		runs++
		attempts[next.Number]++
		skipped[next.Number] = attempts[next.Number] >= l.Limits.MaxAttempts
	}
}

// hand gives task to one unattended agent run. An agent that exits with a
// failure is reported and left to the tracker to judge; only an interrupted
// run is an error.
func (l Loop) hand(ctx context.Context, task tracker.Task, spec int) error {
	if err := l.report("smith: handing #%d %s to the agent\n", task.Number, task.Title); err != nil {
		return err
	}
	err := l.Launcher.Launch(ctx, l.Agent.Unattended(l.Prompt.Render(task, spec)))
	if ctx.Err() != nil {
		return fmt.Errorf("agent on #%d interrupted: %w", task.Number, ctx.Err())
	}
	if err != nil {
		return l.report("smith: the agent on #%d ended with a failure: %v\n", task.Number, err)
	}
	return nil
}

// report writes a progress line.
func (l Loop) report(format string, args ...any) error {
	if _, err := fmt.Fprintf(l.Progress, format, args...); err != nil {
		return fmt.Errorf("write progress: %w", err)
	}
	return nil
}
