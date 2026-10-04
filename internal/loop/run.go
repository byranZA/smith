package loop

import (
	"context"
	"fmt"
	"io"
	"slices"

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
// that is the agent's work, asked for through the prompt.
type Loop struct {
	Tracker  Tracker
	Launcher Launcher
	Agent    agent.Adapter
	Prompt   Prompt
	// Progress is where the loop names each task as it hands it out.
	Progress io.Writer
}

// Outcome is how a run ended: what remained of the spec when it stopped, and
// the task the agent left open, if that is why it stopped.
type Outcome struct {
	Remaining Remaining
	LeftOpen  []tracker.Task
}

// Complete reports whether the run ended with every task of the spec closed.
func (o Outcome) Complete() bool { return o.Remaining.Complete() }

// Run works spec until no task is available for an agent, re-reading the
// tracker before every selection so a task filed mid-run is seen. It stops
// early when the agent leaves the task it was handed open.
func (l Loop) Run(ctx context.Context, spec int) (Outcome, error) {
	var last *tracker.Task
	for {
		current, err := l.Tracker.Spec(ctx, spec)
		if err != nil {
			return Outcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
		}
		remaining := Survey(current)
		if last != nil && stillOpen(current, *last) {
			return Outcome{Remaining: remaining, LeftOpen: []tracker.Task{*last}}, nil
		}
		next, ok := remaining.Next()
		if !ok {
			return Outcome{Remaining: remaining}, nil
		}
		if err := l.hand(ctx, next, spec); err != nil {
			return Outcome{}, err
		}
		last = &next
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

// stillOpen reports whether task is among spec's tasks and still open.
func stillOpen(spec tracker.Spec, task tracker.Task) bool {
	return slices.ContainsFunc(spec.Tasks, func(t tracker.Task) bool { return t.Number == task.Number && t.Open })
}
