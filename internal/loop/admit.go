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

// Admit returns nil when execution may run: a spec whatever its tasks hold,
// and a single task only when it is available. Otherwise it returns a
// *RefusedError naming why the task would not run.
func Admit(execution tracker.Execution) error {
	if !execution.Single {
		return nil
	}
	for _, task := range execution.Tasks {
		if reason := unavailable(task); reason != "" {
			return &RefusedError{Task: task, Reason: reason}
		}
	}
	return nil
}

// unavailable returns why task is not available, in the order Survey sorts
// tasks, and "" when it is.
func unavailable(task tracker.Task) string {
	switch {
	case !task.Open:
		return "is closed"
	case task.For == tracker.Human:
		return "is meant for a human"
	case task.For != tracker.Agent:
		return `is not labelled "ready-for-agent"`
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
