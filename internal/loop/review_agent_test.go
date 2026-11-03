package loop_test

import (
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/review"
	"github.com/byranZA/smith/internal/tracker"
)

func ledgerComments(t *testing.T, done bool) []tracker.Comment {
	t.Helper()
	file, err := review.Parse([]byte(findingsFile("", finding("spec", "major", "agent", "S one"))))
	if err != nil {
		t.Fatal(err)
	}
	body := review.Comment(file, 42, tip, nil)
	if done {
		body = review.CommentDone(file, 42, tip, map[int]int{0: 101})
	}
	return []tracker.Comment{{ID: 8, Body: body}}
}

func TestNeedsReviewAgent(t *testing.T) {
	spec := tracker.Execution{Number: 42, Tasks: []tracker.Task{closed(agentTask(43))}}
	child := tracker.Execution{Number: 42, Single: true, Tasks: []tracker.Task{closed(agentTask(43))}}
	reviewed := spec
	reviewed.Reviewed = true
	resuming := spec
	resuming.Comments = ledgerComments(t, false)
	doneLedger := spec
	doneLedger.Comments = ledgerComments(t, true)
	tests := []struct {
		name      string
		execution tracker.Execution
		want      bool
	}{
		{"an unreviewed spec", spec, true},
		{"a child task run alone", child, false},
		{"an already reviewed execution", reviewed, false},
		{"an incomplete ledger to resume", resuming, false},
		{"a finished ledger whose reviewed mark was removed", doneLedger, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loop.NeedsReviewAgent(tt.execution)
			if err != nil || got != tt.want {
				t.Errorf("NeedsReviewAgent() = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}
