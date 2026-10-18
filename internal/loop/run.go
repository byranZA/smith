package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/tracker"
)

// Tracker is where the loop reads an execution and its tasks, and marks the
// task an agent is working or the loop gave up on, for people to see, with a
// comment on a skipped one. The loop never reads the marks to choose a task,
// only to clear those a dead run left, and never reads its comments back.
type Tracker interface {
	// Execution reads issue number as an execution: a spec and its tasks, or
	// a task alone, as they stand now.
	Execution(ctx context.Context, number int) (tracker.Execution, error)
	// Task reads issue number as a task as it stands now, whatever it has
	// become since.
	Task(ctx context.Context, number int) (tracker.Task, error)
	// Start marks task number as worked, while an agent runs on it.
	Start(ctx context.Context, number int) error
	// Stop removes the mark Start made on task number.
	Stop(ctx context.Context, number int) error
	// Fail removes the mark Start made on task number and hands it to a
	// human, marked as failed.
	Fail(ctx context.Context, number int) error
	// Comment posts body, as the loop wrote it, on issue number.
	Comment(ctx context.Context, number int, body string) error
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
	// Open returns the directory of task's worktree on a branch cut from the
	// work branch's tip, recycling a landed task's worktree, or returns the
	// one an earlier attempt left.
	Open(ctx context.Context, task int) (string, error)
	// Land brings task's commits into the work branch and removes its
	// branch, keeping its worktree for the next task.
	Land(ctx context.Context, task int) error
	// TaskBranch names the branch task's worktree has checked out.
	TaskBranch(task int) string
	// Ahead counts the commits task's branch has beyond the work branch.
	Ahead(ctx context.Context, task int) (int, error)
	// Finish removes the worktree landed tasks were recycled through, once
	// every task has landed.
	Finish(ctx context.Context) error
}

// Preparer is the seam through which the loop makes a task worktree ready to
// work in before each agent attempt.
type Preparer interface {
	// Prepare prepares the worktree at dir, streaming its output to out as it
	// is printed.
	Prepare(ctx context.Context, dir string, out io.Writer) error
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
	// Preparer prepares the task worktree before every agent attempt; nil
	// prepares nothing.
	Preparer Preparer
	Agent    agent.Adapter
	// Options are the model and effort every agent run asks for.
	Options agent.Options
	Prompt  Prompt
	Limits  Limits
	// Progress is where the loop names each task as it hands it out.
	Progress io.Writer
	// Hostname names the machine the loop runs on, for the comment on a
	// skipped task; nil leaves it unknown.
	Hostname func() (string, error)
	// Recorder keeps how each unattended run ended; nil keeps it nowhere.
	Recorder Recorder
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
	// Killed holds each skipped task whose agent was killed by SIGKILL on
	// its last attempt.
	Killed map[int]bool
}

// Complete reports whether the run ended with every task of the spec closed.
func (o Outcome) Complete() bool { return o.Remaining.Complete() }

// Run first clears the worked marks a run that died left on spec's tasks, then
// works spec until no task is available for an agent, handing a task left open
// out again up to MaxAttempts and stopping after MaxIterations agent runs. A
// task left open after its last attempt is skipped and handed to a human,
// with a comment on its issue saying where its work was left. Once spec is
// read, however the run stops, its outcome and stop report go to the
// Recorder and as a comment on spec's issue; a run that cannot read spec at
// all is aborted.
func (l Loop) Run(ctx context.Context, spec int) (Outcome, error) {
	current, err := l.Tracker.Execution(ctx, spec)
	if err != nil {
		failed := fmt.Errorf("read #%d: %w", spec, err)
		return Outcome{}, errors.Join(failed, l.Abort(ctx, spec, failed))
	}
	outcome, err := l.work(ctx, spec, current)
	if reported := l.conclude(ctx, current, outcome, err); reported != nil {
		return outcome, errors.Join(err, reported)
	}
	return outcome, err
}

// work works spec, read as current, as Run says, and returns how it ended,
// removing the recycled task worktree when every task has landed.
func (l Loop) work(ctx context.Context, spec int, current tracker.Execution) (Outcome, error) {
	attempts := map[int]int{}
	skipped := map[int]bool{}
	killed := map[int]bool{}
	worktrees := map[int]string{}
	runs := 0
	if err := l.sweep(ctx, current); err != nil {
		return Outcome{}, err
	}
	for {
		remaining := survey(current, skipped)
		next, ok := remaining.Next()
		if !ok {
			return Outcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch(), Left: left(remaining, worktrees), Killed: killedOf(remaining, killed)}, l.finish(ctx, remaining)
		}
		if runs >= l.Limits.MaxIterations {
			return Outcome{Remaining: remaining, Capped: true, WorkBranch: l.Bench.WorkBranch(), Left: left(remaining, worktrees), Killed: killedOf(remaining, killed)}, nil
		}
		dir, kill, err := l.hand(ctx, next, l.Agent.Unattended(l.Prompt.Render(next), l.Options))
		if err != nil {
			return Outcome{}, err
		}
		worktrees[next.Number] = dir
		killed[next.Number] = kill
		runs++
		attempts[next.Number]++
		skipped[next.Number] = attempts[next.Number] >= l.Limits.MaxAttempts
		if current, err = l.refresh(ctx, spec, current); err != nil {
			return Outcome{}, errors.Join(err, l.unmarkDetached(ctx, next))
		}
		closed := closedIn(current, next)
		skip := skipped[next.Number] && !closed
		if err := l.release(ctx, next, skip); err != nil {
			return Outcome{}, err
		}
		if skip {
			if err := l.noteSkip(ctx, next, dir, attempts[next.Number], kill); err != nil {
				return Outcome{}, err
			}
		}
		if closed {
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
	current, err := l.Tracker.Execution(ctx, spec)
	if err != nil {
		return InteractiveOutcome{}, fmt.Errorf("read #%d: %w", spec, err)
	}
	remaining := Survey(current)
	next, ok := remaining.Next()
	if !ok {
		return InteractiveOutcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch()}, nil
	}
	if _, _, err := l.hand(ctx, next, l.Agent.Interactive(l.Prompt.Interactive().Render(next), l.Options)); err != nil {
		return InteractiveOutcome{}, err
	}
	if err := l.release(ctx, next, false); err != nil {
		return InteractiveOutcome{}, err
	}
	after, err := l.refresh(ctx, spec, current)
	if err != nil {
		return InteractiveOutcome{}, err
	}
	if closedIn(after, next) {
		if err := l.land(ctx, next); err != nil {
			return InteractiveOutcome{}, err
		}
	}
	return InteractiveOutcome{Task: next, Handed: true, Remaining: Survey(after), WorkBranch: l.Bench.WorkBranch()}, nil
}

// refresh reads current, the execution read for issue number, as it stands
// now. A single task keeps its scope: it is read again as that task, so one
// that gains sub-issues or the spec label is never swapped for its children.
func (l Loop) refresh(ctx context.Context, number int, current tracker.Execution) (tracker.Execution, error) {
	if !current.Single || len(current.Tasks) != 1 {
		execution, err := l.Tracker.Execution(ctx, number)
		if err != nil {
			return tracker.Execution{}, fmt.Errorf("read #%d: %w", number, err)
		}
		return execution, nil
	}
	task, err := l.Tracker.Task(ctx, current.Tasks[0].Number)
	if err != nil {
		return tracker.Execution{}, fmt.Errorf("read #%d: %w", current.Tasks[0].Number, err)
	}
	current.Tasks = []tracker.Task{task}
	return current, nil
}

// hand marks task as worked and gives it to one agent run of cmd in task's
// worktree, returning that worktree's directory and whether the agent was
// killed by SIGKILL, and reports an agent that exits with a failure, leaving
// it to the tracker to judge. The worktree is prepared first, and a failed
// preparation is an error naming it before task is marked or any agent runs.
// The caller releases the mark, except on an error from an interrupted run or
// a failed report of that failure, when hand has already removed it.
func (l Loop) hand(ctx context.Context, task tracker.Task, cmd agent.Command) (string, bool, error) {
	dir, err := l.Bench.Open(ctx, task.Number)
	if err != nil {
		return "", false, fmt.Errorf("open a worktree for #%d: %w", task.Number, err)
	}
	cmd.Dir = dir
	if err := l.prepare(ctx, task, dir); err != nil {
		return "", false, err
	}
	if err := l.report("smith: handing #%d %s to the agent in %s\n", task.Number, task.Title, dir); err != nil {
		return "", false, err
	}
	if err := l.warnIf(l.Tracker.Start(ctx, task.Number), "could not mark #%d as worked", task.Number); err != nil {
		return "", false, err
	}
	err = l.Launcher.Launch(ctx, cmd)
	if ctx.Err() != nil {
		interrupted := fmt.Errorf("agent on #%d interrupted: %w", task.Number, ctx.Err())
		return "", false, errors.Join(interrupted, l.unmarkDetached(ctx, task))
	}
	if err == nil {
		return dir, false, nil
	}
	kill := errors.Is(err, ErrKilled)
	if failed := l.reportFailure(task, err, kill); failed != nil {
		return "", false, errors.Join(failed, l.unmarkDetached(ctx, task))
	}
	return dir, kill, nil
}

// prepare prepares task's worktree at dir with the Preparer, when there is
// one, streaming its output to Progress. A failure is an error naming
// preparation, task and dir, since another agent attempt cannot fix it.
func (l Loop) prepare(ctx context.Context, task tracker.Task, dir string) error {
	if l.Preparer == nil {
		return nil
	}
	if err := l.Preparer.Prepare(ctx, dir, l.Progress); err != nil {
		return fmt.Errorf("prepare the worktree of #%d in %s: %w", task.Number, dir, err)
	}
	return nil
}

// reportFailure reports that the agent on task exited with err, naming the
// likely cause instead when the agent was killed.
func (l Loop) reportFailure(task tracker.Task, err error, killed bool) error {
	if killed {
		return l.report("smith: the agent on #%d was %s\n", task.Number, KilledCause)
	}
	return l.report("smith: the agent on #%d ended with a failure: %v\n", task.Number, err)
}

// KilledCause says how an agent killed by SIGKILL ended, and why it likely
// was, for whatever reports it.
const KilledCause = "killed (likely out of memory)"

// sweep removes the mark from each of execution's tasks marked as worked,
// which a run that died left behind, before any agent runs.
func (l Loop) sweep(ctx context.Context, execution tracker.Execution) error {
	for _, task := range execution.Tasks {
		if !task.Working {
			continue
		}
		if err := l.unmark(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// release removes the mark hand made on task once its agent has run, handing
// task to a human instead when the loop skips it.
func (l Loop) release(ctx context.Context, task tracker.Task, skipped bool) error {
	if skipped {
		return l.warnIf(l.Tracker.Fail(ctx, task.Number), "could not hand #%d to a human", task.Number)
	}
	return l.unmark(ctx, task)
}

// unmark removes the mark that says task is worked, warning when it cannot.
func (l Loop) unmark(ctx context.Context, task tracker.Task) error {
	return l.warnIf(l.Tracker.Stop(ctx, task.Number), "could not remove the worked mark from #%d", task.Number)
}

// detachedStopTimeout bounds removing the worked mark from a task on a run
// that is ending in an error, so a hung tracker cannot hold the run up.
const detachedStopTimeout = 10 * time.Second

// unmarkDetached removes the mark that says task is worked as a run ends in
// an error, on a context detached from ctx, the run's, so it runs even once
// ctx is cancelled, and bounded by detachedStopTimeout, warning when it cannot.
func (l Loop) unmarkDetached(ctx context.Context, task tracker.Task) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedStopTimeout)
	defer cancel()
	return l.unmark(ctx, task)
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

// killedOf returns whether the agent was killed on the last attempt at each
// task remaining skipped, from killed, by task number, leaving out the rest.
func killedOf(remaining Remaining, killed map[int]bool) map[int]bool {
	tasks := map[int]bool{}
	for _, task := range remaining.Skipped {
		if killed[task.Number] {
			tasks[task.Number] = true
		}
	}
	return tasks
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

// finish removes the recycled task worktree when remaining is complete, and
// keeps it otherwise for a later run of the execution. A failed removal loses
// no work, so it is a warning, and only a failed progress write is an error.
func (l Loop) finish(ctx context.Context, remaining Remaining) error {
	if !remaining.Complete() {
		return nil
	}
	return l.warnIf(l.Bench.Finish(ctx), "could not remove the recycled task worktree of %s", l.Bench.WorkBranch())
}

// closedIn reports whether spec holds task as closed.
func closedIn(spec tracker.Execution, task tracker.Task) bool {
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
