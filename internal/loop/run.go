package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/tailbuf"
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
	// Post posts body, as the loop wrote it, on issue number and returns the
	// comment's id.
	Post(ctx context.Context, number int, body string) (int64, error)
	// EditComment replaces the body of the comment with the given id.
	EditComment(ctx context.Context, id int64, body string) error
	// File files task as a sub-issue of issue parent and returns its number.
	File(ctx context.Context, parent int, task tracker.NewTask) (int, error)
	// StartReview marks issue number as under review, while its review runs.
	StartReview(ctx context.Context, number int) error
	// StopReview removes the mark StartReview made.
	StopReview(ctx context.Context, number int) error
	// MarkReviewed marks issue number as reviewed, so no run reviews it again.
	MarkReviewed(ctx context.Context, number int) error
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
	// OpenReview returns the directory of the worktree the review runs in:
	// the recycled one, or a new one, detached at the work branch's tip with
	// no branch of its own. issue is the execution's issue.
	OpenReview(ctx context.Context, issue int) (string, error)
	// ReviewBase returns the commit the work branch left its base at, and the
	// work branch's tip: the range the review reads.
	ReviewBase(ctx context.Context) (mergeBase, tip string, err error)
	// ReviewStrays reports what the review left in its worktree dir: the
	// commits HEAD carries beyond tip, the work branch's tip it started at,
	// and the files changed from it.
	ReviewStrays(ctx context.Context, dir, tip string) (Strays, error)
	// FindingsFile is the absolute path the review of issue writes its
	// findings to.
	FindingsFile(issue int) string
	// Finish removes the worktree landed tasks were recycled through, once
	// every task has landed and the review is done.
	Finish(ctx context.Context) error
}

// Strays is what a review left behind in its worktree, which it must not
// change.
type Strays struct {
	// Commits describes each commit HEAD carries beyond the work branch tip,
	// or says HEAD moved off it.
	Commits []string
	// Changed names each file modified, added or removed.
	Changed []string
}

// Any reports whether the review left anything behind.
func (s Strays) Any() bool { return len(s.Commits) > 0 || len(s.Changed) > 0 }

// String says what the review left behind.
func (s Strays) String() string {
	var parts []string
	if len(s.Commits) > 0 {
		parts = append(parts, "it made commits: "+strings.Join(s.Commits, "; "))
	}
	if len(s.Changed) > 0 {
		parts = append(parts, "it left changed files: "+strings.Join(s.Changed, ", "))
	}
	return strings.Join(parts, " and ")
}

// Preparer is the seam through which the loop makes a task worktree ready to
// work in before each agent attempt.
type Preparer interface {
	// Prepare prepares the worktree at dir, streaming its output to out as it
	// is printed.
	Prepare(ctx context.Context, dir string, out io.Writer) error
}

// Stacker is the seam through which the loop brings a repo's stack up for a
// worktree and takes it down.
type Stacker interface {
	// Ensure brings the stack the worktree at dir declares up, streaming its
	// output to out. A worktree that declares none runs nothing.
	Ensure(ctx context.Context, dir string, out io.Writer) error
	// Release takes down the stack Ensure brought up, once, streaming its
	// output to out, even though ctx is cancelled. It does nothing when no
	// stack is up.
	Release(ctx context.Context, out io.Writer) error
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
	// Stack brings the repo's stack up after each worktree is prepared and
	// takes it down when the run ends; nil, as off a box, runs no stack.
	Stack Stacker
	// Placements are put into each task worktree before it is prepared; nil,
	// as off a box, places nothing.
	Placements *Placements
	// Presets chooses the preset each task is handed to, and so its agent,
	// model and effort.
	Presets preset.Choice
	Prompt  Prompt
	// Review turns the review of a complete execution on; nil turns it off.
	Review *Review
	// NoReview reports that the review role is set but --no-review skips it
	// for this run: nothing is reviewed or marked, and the report says so.
	NoReview bool
	Limits   Limits
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
	// MaxAttempts caps the agent runs one task gets before it is skipped, or
	// the run stops when every one of them was an agent failure.
	MaxAttempts int
}

// DefaultLimits are the limits of a run that sets none: ten agent runs in
// all, and two attempts at each task.
func DefaultLimits() Limits { return Limits{MaxIterations: 10, MaxAttempts: 2} }

// Outcome is how a run ended: what remained of the spec when it stopped,
// with the tasks it skipped among it, whether the iteration cap stopped it,
// the work branch its closed tasks landed on, the task worktrees it left, and
// the task it stopped on when its agent looked broken.
type Outcome struct {
	Remaining  Remaining
	Capped     bool
	WorkBranch string
	// Left maps each skipped task to the task worktree left for the operator.
	Left map[int]string
	// Killed holds each skipped task whose agent was killed by SIGKILL on
	// its last attempt.
	Killed map[int]bool
	// Broken is the task the run stopped on as agent broken; nil when it
	// did not stop that way.
	Broken *Broken
	// ReviewOwed reports that every task is closed but the run reached its
	// cap before it could review, so a later run only reviews.
	ReviewOwed bool
	// ReviewFailure is why the review failed, nil when it did not.
	ReviewFailure error
	// ReviewDir is the worktree a failed review was left in.
	ReviewDir string
	// Filed is the tasks the run's review filed, in the order it filed them.
	Filed []Filed
	// ReviewIncomplete is set when a tracker failure stopped the filing of
	// the review's findings.
	ReviewIncomplete *Incomplete
	// Note is something the report says about the review beyond how it ended.
	Note string
	// Hint is a line ending the report that says what to run next.
	Hint string
}

// Filed is a task a review filed for one of its findings.
type Filed struct {
	Number int
	// ID is the finding's id in the review: S1, St1, Q1.
	ID string
}

// Broken is the task a run stopped on because every attempt at it was an
// agent failure, or its agent could not be started, with the last failure
// and the machine the agent failed on.
type Broken struct {
	Task    tracker.Task
	Failure error
	Machine string
	// Review reports that Task is the issue under review, not a task: the
	// failure is of the review's agent.
	Review bool
}

// Complete reports whether the run ended with every task of the spec closed
// and, when it reviews, the review done.
func (o Outcome) Complete() bool {
	return o.Remaining.Complete() && !o.ReviewOwed && o.ReviewFailure == nil && o.ReviewIncomplete == nil && o.Broken == nil
}

// Run first refuses spec when one of its open tasks has a preset the run
// cannot resolve, then clears the worked marks a run that died left on spec's tasks, then
// works spec until no task is available for an agent, handing a task left open
// out again up to MaxAttempts and stopping after MaxIterations agent runs. A
// task left open after its last attempt is skipped and handed to a human,
// with a comment on its issue saying where its work was left, unless every
// attempt at it was an agent failure: the run then stops as agent broken,
// leaving the task unmarked, as it does on an agent that cannot be started
// at all (ADR-0020). Once spec is read, however the run stops, its outcome
// and stop report go to the Recorder and as a comment on spec's issue; a run
// that cannot read spec at all is aborted.
func (l Loop) Run(ctx context.Context, spec int) (Outcome, error) {
	current, err := l.Tracker.Execution(ctx, spec)
	if err != nil {
		failed := fmt.Errorf("read #%d: %w", spec, err)
		return Outcome{}, errors.Join(failed, l.Abort(ctx, spec, failed))
	}
	outcome, err := l.work(ctx, spec, current)
	err = errors.Join(err, l.stackDown(ctx))
	if reported := l.conclude(ctx, current, outcome, err); reported != nil {
		return outcome, errors.Join(err, reported)
	}
	return outcome, err
}

// work works spec, read as current, as Run says, and returns how it ended,
// removing the recycled task worktree when every task has landed.
func (l Loop) work(ctx context.Context, spec int, current tracker.Execution) (Outcome, error) {
	attempts := map[int]int{}
	ran := map[int]bool{}
	skipped := map[int]bool{}
	killed := map[int]bool{}
	worktrees := map[int]string{}
	runs := 0
	reviewedAlready := false
	var filed []Filed
	var note, hint string
	stopped := func(remaining Remaining) Outcome {
		return Outcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch(), Left: left(remaining, worktrees), Killed: killedOf(remaining, killed), Filed: filed, Note: note, Hint: hint}
	}
	if err := l.Presets.Check(current); err != nil {
		return Outcome{}, fmt.Errorf("choose the presets of %s: %w", Name(current), err)
	}
	pending, pendingID, resuming, err := l.ledgerOf(current)
	if err != nil {
		return Outcome{}, err
	}
	if err := l.sweep(ctx, current); err != nil {
		return Outcome{}, err
	}
	for {
		remaining := survey(current, skipped)
		if resuming {
			resuming = false
			result, err := l.resume(ctx, current, pending, pendingID)
			if err != nil {
				return Outcome{}, err
			}
			reviewedAlready = true
			filed, note = result.filed, result.note
			if result.incomplete != nil {
				return l.reviewStopped(ctx, stopped(remaining), current, result)
			}
			if current, err = l.reread(ctx, spec, current); err != nil {
				return Outcome{}, err
			}
			continue
		}
		next, ok := remaining.Next()
		if !ok && l.reviewDue(current, remaining, reviewedAlready) {
			result, err := l.review(ctx, current, &runs)
			if err != nil {
				return Outcome{}, err
			}
			filed = result.filed
			if result.failure != nil || result.capped || result.incomplete != nil {
				return l.reviewStopped(ctx, stopped(remaining), current, result)
			}
			reviewedAlready = true
			if current, err = l.reread(ctx, spec, current); err != nil {
				return Outcome{}, err
			}
			continue
		}
		if !ok {
			if l.NoReview && reviewable(current) && remaining.Complete() {
				note = "not reviewed: `--no-review`"
			}
			hint = l.reviewHint(ctx, current, remaining)
			return stopped(remaining), l.finish(ctx, remaining)
		}
		if runs >= l.Limits.MaxIterations {
			capped := stopped(remaining)
			capped.Capped = true
			return capped, nil
		}
		adapter, resolved, err := l.agent(next)
		if err != nil {
			return Outcome{}, err
		}
		run, err := l.hand(ctx, next, resolved, adapter.Unattended(l.Prompt.Render(next), resolved.Options()))
		if err != nil {
			return Outcome{}, err
		}
		if errors.Is(run.failure, ErrNotStarted) {
			return l.broken(ctx, stopped(remaining), next, run.failure)
		}
		worktrees[next.Number] = run.dir
		killed[next.Number] = run.killed()
		ran[next.Number] = ran[next.Number] || !run.agentFailure()
		runs++
		attempts[next.Number]++
		exhausted := attempts[next.Number] >= l.Limits.MaxAttempts
		if current, err = l.refresh(ctx, spec, current); err != nil {
			return Outcome{}, errors.Join(err, l.unmarkDetached(ctx, next))
		}
		closed := closedIn(current, next)
		if exhausted && !closed && !ran[next.Number] {
			return l.broken(ctx, stopped(survey(current, skipped)), next, run.failure)
		}
		skipped[next.Number] = exhausted
		skip := exhausted && !closed
		if err := l.release(ctx, next, skip); err != nil {
			return Outcome{}, err
		}
		if skip {
			if err := l.noteSkip(ctx, next, run, attempts[next.Number]); err != nil {
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

// reviewStopped ends a run whose review stopped as result says: failed, which
// keeps the review worktree for the operator, or capped before it could run,
// when every task has landed and the recycled worktree is removed.
func (l Loop) reviewStopped(ctx context.Context, outcome Outcome, execution tracker.Execution, result reviewed) (Outcome, error) {
	if result.broken {
		number, _ := execution.Issue()
		return l.brokenOn(outcome, tracker.Task{Number: number}, result.failure, true)
	}
	if result.capped {
		outcome.Capped, outcome.ReviewOwed = true, true
		return outcome, l.finish(ctx, outcome.Remaining)
	}
	outcome.Filed = result.filed
	if result.incomplete != nil {
		outcome.ReviewIncomplete = result.incomplete
		return outcome, nil
	}
	outcome.ReviewFailure, outcome.ReviewDir = result.failure, result.dir
	return outcome, nil
}

// broken ends a run that stopped as outcome on task, whose agent last failed
// with failure: it removes task's worked mark, leaving task for the next run
// rather than handing it to a human, and names this machine, unknown with a
// warning when its hostname cannot be read.
func (l Loop) broken(ctx context.Context, outcome Outcome, task tracker.Task, failure error) (Outcome, error) {
	if err := l.unmark(ctx, task); err != nil {
		return Outcome{}, err
	}
	return l.brokenOn(outcome, task, failure, false)
}

// brokenOn ends a run as outcome with its agent broken on task, the issue
// under review when review is set, naming this machine.
func (l Loop) brokenOn(outcome Outcome, task tracker.Task, failure error, review bool) (Outcome, error) {
	host, err := l.hostname()
	if err := l.warnIf(err, "could not read this machine's hostname"); err != nil {
		return Outcome{}, err
	}
	outcome.Broken = &Broken{Task: task, Failure: failure, Machine: host, Review: review}
	return outcome, nil
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

// RunInteractive hands the task Pairing chooses from spec to one agent run
// attached to the operator's terminal, on the task's own preset, and runs no
// agent when there is none or one of spec's open tasks has a preset the run
// cannot resolve. When the spec is complete once it ends, the recycled task
// worktree is removed.
func (l Loop) RunInteractive(ctx context.Context, spec int) (InteractiveOutcome, error) {
	outcome, err := l.interactive(ctx, spec)
	return outcome, errors.Join(err, l.stackDown(ctx))
}

// interactive runs RunInteractive's task, leaving the stack it brought up to
// the caller to take down.
func (l Loop) interactive(ctx context.Context, spec int) (InteractiveOutcome, error) {
	current, err := l.Tracker.Execution(ctx, spec)
	if err != nil {
		return InteractiveOutcome{}, fmt.Errorf("read #%d: %w", spec, err)
	}
	if err := l.Presets.Check(current); err != nil {
		return InteractiveOutcome{}, fmt.Errorf("choose the presets of %s: %w", Name(current), err)
	}
	remaining := Survey(current)
	next, ok := Pairing(current)
	if !ok {
		return InteractiveOutcome{Remaining: remaining, WorkBranch: l.Bench.WorkBranch()}, l.finish(ctx, remaining)
	}
	adapter, resolved, err := l.agent(next)
	if err != nil {
		return InteractiveOutcome{}, err
	}
	if _, err := l.hand(ctx, next, resolved, adapter.Interactive(l.Prompt.Interactive().Render(next), resolved.Options())); err != nil {
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
	remaining = Survey(after)
	return InteractiveOutcome{Task: next, Handed: true, Remaining: remaining, WorkBranch: l.Bench.WorkBranch()}, l.finish(ctx, remaining)
}

// agent returns the preset task is handed to and the adapter of its agent,
// refusing a preset the run cannot resolve.
func (l Loop) agent(task tracker.Task) (agent.Adapter, preset.Resolved, error) {
	resolved, err := l.Presets.Task(task)
	if err != nil {
		return nil, preset.Resolved{}, fmt.Errorf("choose a preset: %w", err)
	}
	adapter, err := agent.Lookup(resolved.Agent.Value)
	if err != nil {
		return nil, preset.Resolved{}, fmt.Errorf("choose the agent of #%d: %w", task.Number, err)
	}
	return adapter, resolved, nil
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

// reread reads current again after a review, which may have filed tasks under
// it. A standalone task is read as an execution again, not as that task: with
// tasks filed under it, it now reads as a spec of the same number, on the same
// work branch, and its own issue stays closed.
func (l Loop) reread(ctx context.Context, number int, current tracker.Execution) (tracker.Execution, error) {
	if !current.Standalone() {
		return l.refresh(ctx, number, current)
	}
	execution, err := l.Tracker.Execution(ctx, number)
	if err != nil {
		return tracker.Execution{}, fmt.Errorf("read #%d: %w", number, err)
	}
	return execution, nil
}

// reviewHint returns the line telling the operator that running the spec
// reviews it, when current is a child task run on its own that leaves its
// spec with every task closed and unreviewed, and the review role is set.
// A spec that cannot be read leaves no hint, with a warning.
func (l Loop) reviewHint(ctx context.Context, current tracker.Execution, remaining Remaining) string {
	if l.Review == nil || !current.Single || current.Standalone() || !remaining.Complete() {
		return ""
	}
	spec, err := l.Tracker.Execution(ctx, current.Number)
	if err != nil {
		_ = l.warnIf(err, "could not read spec #%d to see whether it is ready for review", current.Number)
		return ""
	}
	if spec.Reviewed || !Survey(spec).Complete() {
		return ""
	}
	return fmt.Sprintf("spec #%d has every task closed: `smith loop run %d` reviews it", current.Number, current.Number)
}

// attempt is how one agent run on a task went: the task worktree it ran in,
// the preset it ran on, and Launch's error, nil when the agent exited cleanly.
type attempt struct {
	dir     string
	preset  preset.Resolved
	failure error
}

// killed reports whether the agent was killed by SIGKILL.
func (a attempt) killed() bool { return errors.Is(a.failure, ErrKilled) }

// agentFailure reports whether the agent CLI itself ended the run with a
// failure, rather than exiting cleanly or being killed.
func (a attempt) agentFailure() bool { return a.failure != nil && !a.killed() }

// hand marks task as worked and gives it to one agent run of cmd, on
// resolved, in task's worktree, returning how the attempt went, and reports an agent that fails,
// leaving it to the caller to judge. The repo's files are placed into the worktree, then it is
// prepared, and a failed placement or preparation is an error naming it before
// task is marked or any agent runs.
// The caller releases the mark, except on an error from an interrupted run or
// a failed report of that failure, when hand has already removed it.
func (l Loop) hand(ctx context.Context, task tracker.Task, resolved preset.Resolved, cmd agent.Command) (attempt, error) {
	dir, err := l.Bench.Open(ctx, task.Number)
	if err != nil {
		return attempt{}, fmt.Errorf("open a worktree for #%d: %w", task.Number, err)
	}
	cmd.Dir = dir
	if err := l.Placements.into(ctx, dir); err != nil {
		return attempt{}, fmt.Errorf("place files into the worktree of #%d in %s: %w", task.Number, dir, err)
	}
	if err := l.prepare(ctx, task, dir); err != nil {
		return attempt{}, err
	}
	if err := l.report("smith: handing #%d %s to %s on %s in %s\n", task.Number, task.Title, resolved.Agent.Value, presetPhrase(resolved), dir); err != nil {
		return attempt{}, err
	}
	if err := l.warnIf(l.Tracker.Start(ctx, task.Number), "could not mark #%d as worked", task.Number); err != nil {
		return attempt{}, err
	}
	run := attempt{dir: dir, preset: resolved, failure: l.Launcher.Launch(ctx, cmd)}
	if ctx.Err() != nil {
		interrupted := fmt.Errorf("agent on #%d interrupted: %w", task.Number, ctx.Err())
		return attempt{}, errors.Join(interrupted, l.unmarkDetached(ctx, task))
	}
	if run.failure == nil {
		return run, nil
	}
	if err := l.reportFailure(task, run.failure, run.killed()); err != nil {
		return attempt{}, errors.Join(err, l.unmarkDetached(ctx, task))
	}
	return run, nil
}

// presetPhrase names resolved as the hand-off line does: "preset opus", or
// "a one-off preset" for one built from the run flags.
func presetPhrase(resolved preset.Resolved) string {
	if resolved.Name == "" {
		return "a one-off preset"
	}
	return "preset " + resolved.Name
}

// PrepareTailLimit bounds how much of a preparation's output the loop keeps
// to show if a step fails.
const PrepareTailLimit = 64 << 10

// prepare prepares task's worktree at dir with the Preparer, when there is
// one, holding its output back and printing the last PrepareTailLimit bytes
// only on failure. A failure is an error naming task and dir, since another
// agent attempt cannot fix it.
func (l Loop) prepare(ctx context.Context, task tracker.Task, dir string) error {
	if l.Preparer != nil {
		if err := l.report("smith: preparing the worktree of #%d\n", task.Number); err != nil {
			return err
		}
		captured := tailbuf.Buffer{Limit: PrepareTailLimit}
		if err := l.Preparer.Prepare(ctx, dir, &captured); err != nil {
			if rerr := l.reportTail(&captured); rerr != nil {
				return rerr
			}
			return fmt.Errorf("prepare the worktree of #%d in %s: %w", task.Number, dir, err)
		}
	}
	return l.stackUp(ctx, task, dir)
}

// stackUp brings the stack of the prepared worktree at dir up with the Stack,
// when there is one, as prepare holds its output back. A failure is an error
// naming task and dir, since another agent attempt cannot fix it.
func (l Loop) stackUp(ctx context.Context, task tracker.Task, dir string) error {
	if l.Stack == nil {
		return nil
	}
	if err := l.report("smith: bringing up the stack of #%d\n", task.Number); err != nil {
		return err
	}
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	if err := l.Stack.Ensure(ctx, dir, &captured); err != nil {
		if rerr := l.reportTail(&captured); rerr != nil {
			return rerr
		}
		return fmt.Errorf("bring up the stack of #%d in %s: %w (system packages and groups come from the blueprint, not the repo file)", task.Number, dir, err)
	}
	return nil
}

// stackDown takes down the stack the run brought up, when there is one,
// warning with the last of down's output when it fails, since the run's
// outcome stands whether or not the stack stopped.
func (l Loop) stackDown(ctx context.Context) error {
	if l.Stack == nil {
		return nil
	}
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	err := l.Stack.Release(ctx, &captured)
	if err == nil {
		return nil
	}
	if werr := l.warnIf(err, "the stack did not stop"); werr != nil {
		return werr
	}
	return l.reportTail(&captured)
}

// reportTail writes the output captured in b to Progress. When b dropped
// earlier output, it starts at the next full line, after a note saying so.
func (l Loop) reportTail(b *tailbuf.Buffer) error {
	out := b.Lines()
	if b.Dropped() {
		if err := l.report("smith: earlier preparation output was cut; showing the last %d KiB\n", PrepareTailLimit>>10); err != nil {
			return err
		}
	}
	if _, err := l.Progress.Write(out); err != nil {
		return fmt.Errorf("write progress: %w", err)
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

// sweep removes the mark from each of execution's tasks marked as worked, and
// the review mark from its issue, which a run that died left behind, before
// any agent runs.
func (l Loop) sweep(ctx context.Context, execution tracker.Execution) error {
	if execution.Reviewing {
		number, _ := execution.Issue()
		if err := l.warnIf(l.Tracker.StopReview(ctx, number), "could not remove the review mark from #%d", number); err != nil {
			return err
		}
	}
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
	if err := l.stackDown(ctx); err != nil {
		return err
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
