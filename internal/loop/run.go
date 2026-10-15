package loop

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/tracker"
)

// Tracker is where the loop reads a spec and its tasks, and marks the task an
// agent is working for people to see. The loop never reads the mark back.
type Tracker interface {
	// Spec reads spec number and its tasks as they stand now.
	Spec(ctx context.Context, number int) (tracker.Spec, error)
	// Start marks task number as worked, while an agent runs on it.
	Start(ctx context.Context, number int) error
	// Stop removes the mark Start made on task number.
	Stop(ctx context.Context, number int) error
}

// Launcher is the one seam through which the loop starts a process.
type Launcher interface {
	// Launch runs cmd to completion, streaming its output to the operator.
	Launch(ctx context.Context, cmd agent.Command) error
}

// Bench is the seam through which the loop reaches the git an execution
// works in: the work branch, and the task worktree each task is worked in.
type Bench interface {
	// WorkBranch names the branch closed tasks land on.
	WorkBranch() string
	// Open returns the directory of task's worktree, creating it from the
	// work branch's tip or returning the one an earlier attempt left.
	Open(ctx context.Context, task int) (string, error)
	// Land brings task's commits into the work branch and removes its
	// worktree and branch.
	Land(ctx context.Context, task int) error
}

// Pusher is the seam through which the loop pushes the work branch.
type Pusher interface {
	// Push pushes branch to origin under its own name, never forcing.
	Push(ctx context.Context, branch string) error
}

// Loop works one spec's tasks with a coding agent, one task per agent run.
// Each task is worked in its own worktree from Bench, never in the operator's
// checkout. The loop never commits: that is the agent's work, asked for
// through the prompt, so a failed attempt's changes are still in the worktree
// for the next. Each task the agent closes is landed on the work branch,
// which is then pushed through Pusher, when one is set.
type Loop struct {
	Tracker  Tracker
	Launcher Launcher
	Bench    Bench
	// Pusher pushes the work branch after each landed task; nil turns
	// pushing off.
	Pusher Pusher
	Agent  agent.Adapter
	// Options are the model and effort every agent run asks for.
	Options agent.Options
	Prompt  Prompt
	Limits  Limits
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
// with the tasks it skipped among it, whether the iteration cap stopped it,
// the work branch its closed tasks landed on, and the task worktrees it left.
type Outcome struct {
	Remaining  Remaining
	Capped     bool
	WorkBranch string
	// Left maps each skipped task to the task worktree left for the operator.
	Left map[int]string
}

// Complete reports whether the run ended with every task of the spec closed.
func (o Outcome) Complete() bool { return o.Remaining.Complete() }

// Run works spec until no task is available for an agent, handing a task left
// open out again up to MaxAttempts and stopping after MaxIterations agent runs.
func (l Loop) Run(ctx context.Context, spec int) (Outcome, error) {
	attempts := map[int]int{}
	skipped := map[int]bool{}
	worktrees := map[int]string{}
	runs := 0
	current, err := l.Tracker.Spec(ctx, spec)
	if err != nil {
		return Outcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
	}
	for {
		remaining := Survey(current).Skip(skipped)
		next, ok := remaining.Next()
		if !ok {
			return Outcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch(), Left: left(remaining, worktrees)}, nil
		}
		if runs >= l.Limits.MaxIterations {
			return Outcome{Remaining: remaining, Capped: true, WorkBranch: l.Bench.WorkBranch(), Left: left(remaining, worktrees)}, nil
		}
		dir, err := l.hand(ctx, next, l.Agent.Unattended(l.Prompt.Render(next), l.Options))
		if err != nil {
			return Outcome{}, err
		}
		worktrees[next.Number] = dir
		runs++
		attempts[next.Number]++
		skipped[next.Number] = attempts[next.Number] >= l.Limits.MaxAttempts
		if current, err = l.Tracker.Spec(ctx, spec); err != nil {
			return Outcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
		}
		if closedIn(current, next) {
			if err := l.land(ctx, next); err != nil {
				return Outcome{}, err
			}
		}
	}
}

// InteractiveOutcome is how an interactive run ended: the task it handed the
// agent, if any, and what remained of the spec once the agent exited.
type InteractiveOutcome struct {
	// Task is the task handed to the agent, when Handed.
	Task tracker.Task
	// Handed reports whether a task was available to hand to the agent.
	Handed bool
	// Remaining is the spec as the tracker told it after the run.
	Remaining Remaining
	// WorkBranch is the branch a closed task landed on.
	WorkBranch string
}

// Closed reports whether the run handed a task and the agent closed it.
func (o InteractiveOutcome) Closed() bool {
	return o.Handed && slices.ContainsFunc(o.Remaining.Closed, func(t tracker.Task) bool { return t.Number == o.Task.Number })
}

// Succeeded reports whether the run closed the task it handed, or, with
// nothing to hand, found the spec complete.
func (o InteractiveOutcome) Succeeded() bool {
	if o.Handed {
		return o.Closed()
	}
	return o.Remaining.Complete()
}

// RunInteractive hands spec's next available task to one agent run attached to
// the operator's terminal, and runs no agent when no task is available.
func (l Loop) RunInteractive(ctx context.Context, spec int) (InteractiveOutcome, error) {
	current, err := l.Tracker.Spec(ctx, spec)
	if err != nil {
		return InteractiveOutcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
	}
	remaining := Survey(current)
	next, ok := remaining.Next()
	if !ok {
		return InteractiveOutcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch()}, nil
	}
	if _, err := l.hand(ctx, next, l.Agent.Interactive(l.Prompt.Interactive().Render(next), l.Options)); err != nil {
		return InteractiveOutcome{}, err
	}
	after, err := l.Tracker.Spec(ctx, spec)
	if err != nil {
		return InteractiveOutcome{}, fmt.Errorf("read spec #%d: %w", spec, err)
	}
	if closedIn(after, next) {
		if err := l.land(ctx, next); err != nil {
			return InteractiveOutcome{}, err
		}
	}
	return InteractiveOutcome{Task: next, Handed: true, Remaining: Survey(after), WorkBranch: l.Bench.WorkBranch()}, nil
}

// hand gives task to one agent run of cmd in task's worktree, marking task as
// worked while the agent runs, and returns that worktree's directory. An agent
// that exits with a failure is reported and left to the tracker to judge; only
// an interrupted run is an error.
func (l Loop) hand(ctx context.Context, task tracker.Task, cmd agent.Command) (string, error) {
	dir, err := l.Bench.Open(ctx, task.Number)
	if err != nil {
		return "", fmt.Errorf("open a worktree for #%d: %w", task.Number, err)
	}
	cmd.Dir = dir
	if err := l.report("smith: handing #%d %s to the agent in %s\n", task.Number, task.Title, dir); err != nil {
		return "", err
	}
	if err := l.warnIf(l.Tracker.Start(ctx, task.Number), "could not mark #%d as worked", task.Number); err != nil {
		return "", err
	}
	err = l.Launcher.Launch(ctx, cmd)
	if ctx.Err() != nil {
		return "", fmt.Errorf("agent on #%d interrupted: %w", task.Number, ctx.Err())
	}
	if err := l.warnIf(l.Tracker.Stop(ctx, task.Number), "could not remove the worked mark from #%d", task.Number); err != nil {
		return "", err
	}
	if err != nil {
		return dir, l.report("smith: the agent on #%d ended with a failure: %v\n", task.Number, err)
	}
	return dir, nil
}

// left returns the worktree of each task remaining skipped, from worktrees,
// by task number.
func left(remaining Remaining, worktrees map[int]string) map[int]string {
	dirs := map[int]string{}
	for _, task := range remaining.Skipped {
		dirs[task.Number] = worktrees[task.Number]
	}
	return dirs
}

// land brings closed task's work onto the work branch and pushes that, when
// the loop has a Pusher, reporting the push. A failed landing or push is an
// error naming task.
func (l Loop) land(ctx context.Context, task tracker.Task) error {
	if err := l.Bench.Land(ctx, task.Number); err != nil {
		return fmt.Errorf("land the work on #%d: %w", task.Number, err)
	}
	if l.Pusher == nil {
		return nil
	}
	branch := l.Bench.WorkBranch()
	if err := l.Pusher.Push(ctx, branch); err != nil {
		return fmt.Errorf("push the work on #%d: %w", task.Number, err)
	}
	return l.report("smith: pushed #%d on %s to origin\n", task.Number, branch)
}

// closedIn reports whether spec holds task as closed.
func closedIn(spec tracker.Spec, task tracker.Task) bool {
	return slices.ContainsFunc(spec.Tasks, func(t tracker.Task) bool { return t.Number == task.Number && !t.Open })
}

// warnIf reports failed as a warning line led by what the format says, when
// failed is not nil. A label write that fails loses no work, so it never
// stops a run; only a failed progress write is an error.
func (l Loop) warnIf(failed error, format string, args ...any) error {
	if failed == nil {
		return nil
	}
	return l.report("smith: warning: %s: %v\n", fmt.Sprintf(format, args...), failed)
}

// report writes a progress line.
func (l Loop) report(format string, args ...any) error {
	if _, err := fmt.Fprintf(l.Progress, format, args...); err != nil {
		return fmt.Errorf("write progress: %w", err)
	}
	return nil
}
