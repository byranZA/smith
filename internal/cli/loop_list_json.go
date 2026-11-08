package cli

import (
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// loopListDocument is the JSON document `loop list --json` prints.
type loopListDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Spec          int            `json:"spec"`
	WorkBranch    string         `json:"work_branch"`
	Next          *int           `json:"next"`
	Tasks         []loopListTask `json:"tasks"`
}

// loopListTask is one task in the document.
type loopListTask struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Labels    []string `json:"labels"`
	State     string   `json:"state"`
	BlockedBy []int    `json:"blocked_by"`
}

// newLoopListDocument shapes the execution and its survey as the document, with empty lists rather than null.
func newLoopListDocument(execution tracker.Execution, remaining loop.Remaining, workBranch string) loopListDocument {
	doc := loopListDocument{SchemaVersion: 1, Spec: execution.Number, WorkBranch: workBranch, Tasks: make([]loopListTask, 0, len(execution.Tasks))}
	if next, ok := remaining.Next(); ok {
		doc.Next = &next.Number
	}
	states := map[int]string{}
	for state, tasks := range map[string][]tracker.Task{
		"available": remaining.Available, "blocked": remaining.Blocked, "human": remaining.Human,
		"unready": remaining.Unready, "closed": remaining.Closed,
	} {
		for _, t := range tasks {
			states[t.Number] = state
		}
	}
	for _, t := range execution.Tasks {
		doc.Tasks = append(doc.Tasks, loopListTask{
			Number: t.Number, Title: t.Title, Labels: nonNil(t.Labels), State: states[t.Number], BlockedBy: openBlockers(t),
		})
	}
	return doc
}

// nonNil returns names, or an empty list when it is nil.
func nonNil(names []string) []string {
	return append([]string{}, names...)
}

// openBlockers returns the numbers of task's blockers that are still open, as a list that is never nil.
func openBlockers(task tracker.Task) []int {
	numbers := []int{}
	for _, b := range task.Blockers {
		if b.Open {
			numbers = append(numbers, b.Number)
		}
	}
	return numbers
}
