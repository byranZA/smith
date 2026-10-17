package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

// Ending is the one outcome a run stops with, as recorded and posted.
type Ending string

// The ways an unattended run can stop.
const (
	// Complete is a run that closed every task.
	Complete Ending = "complete"
	// WaitingOnHuman is a run with no task left for an agent, only ones that
	// are blocked, meant for a human, or not ready.
	WaitingOnHuman Ending = "waiting on human"
	// CapReached is a run stopped by its cap of agent runs.
	CapReached Ending = "cap reached"
	// AttemptsExhausted is a run that skipped tasks until none was available.
	AttemptsExhausted Ending = "attempts exhausted"
	// Stopped is a run interrupted by a signal.
	Stopped Ending = "stopped"
	// Failed is a run stopped on an error.
	Failed Ending = "failed"
)

// Recorder is where a run records how it ended.
type Recorder interface {
	// Record keeps ending and the stop report that says it.
	Record(ending Ending, report string) error
}

// Ending returns the outcome a run that ended as o stopped with.
func (o Outcome) Ending() Ending {
	switch {
	case o.Complete():
		return Complete
	case o.Capped:
		return CapReached
	case len(o.Remaining.Skipped) > 0:
		return AttemptsExhausted
	default:
		return WaitingOnHuman
	}
}

// Report renders how the run of the execution called name ended: complete,
// or why it stopped and what remains open, naming the tasks it skipped, the
// attempts limits allowed each, the task worktree each was left in, and
// whether its agent was killed on its last attempt.
func (o Outcome) Report(name string, limits Limits) string {
	if o.Complete() {
		return name + " complete\n"
	}
	var b strings.Builder
	switch {
	case o.Capped:
		fmt.Fprintf(&b, "%s stopped: reached the cap of %d agent runs\n", name, limits.MaxIterations)
	case len(o.Remaining.Skipped) > 0:
		fmt.Fprintf(&b, "%s stopped: the agent left a task open on every attempt\n", name)
	default:
		fmt.Fprintf(&b, "%s stopped: no task is available for an agent\n", name)
	}
	b.WriteString(Kinds(o.Remaining, limits, false))
	for _, task := range o.Remaining.Skipped {
		fmt.Fprintf(&b, "  #%d left in %s", task.Number, o.Left[task.Number])
		if o.Killed[task.Number] {
			b.WriteString(": the agent was " + KilledCause)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// WorkBranchReport renders the line naming the work branch a run's closed
// tasks land on.
func WorkBranchReport(branch string) string {
	return fmt.Sprintf("work branch: %s\n", branch)
}

// Name names execution in a report as what it is: "task #80" for a task
// named on its own, "spec #42" otherwise.
func Name(execution tracker.Execution) string {
	number, _ := execution.Issue()
	if execution.Single {
		return fmt.Sprintf("task #%d", number)
	}
	return fmt.Sprintf("spec #%d", number)
}

// Kinds renders one line for each kind of task that remains, leaving out the
// kinds it has none of, and the closed tasks unless withClosed is set.
// Skipped tasks are named with the attempts limits allowed them.
func Kinds(remaining Remaining, limits Limits, withClosed bool) string {
	var closed []tracker.Task
	if withClosed {
		closed = remaining.Closed
	}
	var b strings.Builder
	for _, kind := range []struct {
		name  string
		tasks []tracker.Task
	}{
		{"available", remaining.Available},
		{fmt.Sprintf("skipped after %d attempts", limits.MaxAttempts), remaining.Skipped},
		{"blocked", remaining.Blocked},
		{"waiting on a human", remaining.Human},
		{"not ready for an agent or a human", remaining.Unready},
		{"closed", closed},
	} {
		if len(kind.tasks) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", kind.name, taskList(kind.tasks))
		}
	}
	return b.String()
}

// taskList renders tasks as "#43, #44", naming each blocked task's open
// blockers after it.
func taskList(tasks []tracker.Task) string {
	refs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ref := fmt.Sprintf("#%d", task.Number)
		var open []string
		for _, b := range task.Blockers {
			if b.Open {
				open = append(open, tracker.Ref{Repo: b.Repo, Number: b.Number}.String())
			}
		}
		if len(open) > 0 {
			ref += " (by " + strings.Join(open, ", ") + ")"
		}
		refs = append(refs, ref)
	}
	return strings.Join(refs, ", ")
}

// conclude hands how the run of execution ended, with outcome or the error
// failed, to the Recorder when there is one and posts its stop report on the
// execution's issue, on a context detached from ctx so an interrupted run
// still reports, warning when either fails.
func (l Loop) conclude(ctx context.Context, execution tracker.Execution, outcome Outcome, failed error) error {
	ending, report := l.stopReport(ctx, Name(execution), outcome, failed)
	report += WorkBranchReport(l.Bench.WorkBranch())
	if l.Recorder != nil {
		if err := l.warnIf(l.Recorder.Record(ending, report), "could not record how the run ended"); err != nil {
			return err
		}
	}
	number, _ := execution.Issue()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedStopTimeout)
	defer cancel()
	return l.warnIf(l.Tracker.Comment(ctx, number, stopComment(execution, ending, report)), "could not post the stop report on #%d", number)
}

// Abort concludes a run of issue that failed with failed before it could
// start working: it records the run as Failed with the Recorder when there is
// one and, when there is a Tracker, posts the stop report on issue, on a
// context detached from ctx, warning when either fails.
func (l Loop) Abort(ctx context.Context, issue int, failed error) error {
	name := fmt.Sprintf("#%d", issue)
	report := fmt.Sprintf("%s failed: %v\n", name, failed)
	if l.Recorder != nil {
		if err := l.warnIf(l.Recorder.Record(Failed, report), "could not record how the run ended"); err != nil {
			return err
		}
	}
	if l.Tracker == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedStopTimeout)
	defer cancel()
	body := fmt.Sprintf("smith stopped its run of %s: **%s**.\n\n```text\n%s```\n", name, Failed, report)
	return l.warnIf(l.Tracker.Comment(ctx, issue, body), "could not post the stop report on #%d", issue)
}

// stopReport returns the outcome and the report of the run of the execution
// called name that ended with outcome, or with the error failed: stopped
// when ctx, the run's, was cancelled, and failed otherwise.
func (l Loop) stopReport(ctx context.Context, name string, outcome Outcome, failed error) (Ending, string) {
	switch {
	case failed == nil:
		return outcome.Ending(), outcome.Report(name, l.Limits)
	case ctx.Err() != nil:
		return Stopped, name + " stopped: interrupted\n"
	default:
		return Failed, fmt.Sprintf("%s failed: %v\n", name, failed)
	}
}

// stopComment renders the comment that posts report, of a run of execution
// that stopped with ending, on its issue.
func stopComment(execution tracker.Execution, ending Ending, report string) string {
	kind := "spec"
	if execution.Single {
		kind = "task"
	}
	return fmt.Sprintf("smith stopped its run of this %s: **%s**.\n\n```text\n%s```\n", kind, ending, report)
}
