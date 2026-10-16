package loop

import (
	"fmt"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

// RefusedError reports a task named on its own that is not available, and
// why: closed, meant for a human, not labelled for an agent, or blocked.
type RefusedError struct {
	Task   tracker.Task
	Reason string
}

// Error implements error.
func (e *RefusedError) Error() string { return fmt.Sprintf("#%d %s", e.Task.Number, e.Reason) }

// EmptyError reports a spec not yet split into tasks, which leaves nothing to
// hand to an agent.
type EmptyError struct {
	Number int
}

// Error implements error.
func (e *EmptyError) Error() string { return fmt.Sprintf("#%d has no tasks", e.Number) }

// Admit returns nil when execution may run: a spec with at least one task,
// whatever they hold, and a single task only when it is available. Otherwise
// it returns an *EmptyError for a spec with no tasks, or a *RefusedError naming
// why the task would not run, in the words of labels.
func Admit(execution tracker.Execution, labels tracker.Labels) error {
	if !execution.Single {
		if len(execution.Tasks) == 0 {
			return &EmptyError{Number: execution.Number}
		}
		return nil
	}
	for _, task := range execution.Tasks {
		if reason := unavailable(task, labels); reason != "" {
			return &RefusedError{Task: task, Reason: reason}
		}
	}
	return nil
}

// unavailable returns why task is not available, in the order Survey sorts
// tasks and naming the agent label of labels, and "" when it is.
func unavailable(task tracker.Task, labels tracker.Labels) string {
	switch {
	case !task.Open:
		return "is closed"
	case task.For == tracker.Human:
		return "is meant for a human"
	case task.For != tracker.Agent:
		return fmt.Sprintf("is not labelled %q", labels.Agent)
	case blocked(task):
		return "is blocked by " + strings.Join(openBlockers(task), ", ")
	default:
		return ""
	}
}

// openBlockers names task's blockers that are still open, as "#79" or "o/x#3".
func openBlockers(task tracker.Task) []string {
	var open []string
	for _, b := range task.Blockers {
		if b.Open {
			open = append(open, tracker.Ref{Repo: b.Repo, Number: b.Number}.String())
		}
	}
	return open
}
