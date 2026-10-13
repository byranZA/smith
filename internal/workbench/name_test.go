package workbench_test

import (
	"testing"

	"github.com/byranZA/smith/internal/workbench"
)

func TestBranchNamesTheWorkBranchAfterTheIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		number int
		title  string
		want   string
	}{
		{"plain title", 430, "Loop works in task worktrees", "smith/430-loop-works-in-task-worktrees"},
		{"leading Spec: dropped", 419, "Spec: the loop works in task worktrees", "smith/419-the-loop-works-in-task-worktrees"},
		{"punctuation runs become one hyphen", 7, "Fix `Push` -- don't force!", "smith/7-fix-push-don-t-force"},
		{"long title cut at a word", 8, "the loop works each task in a worktree off a new work branch", "smith/8-the-loop-works-each-task-in-a-worktree"},
		{"long word cut at the bound", 9, "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz", "smith/9-abcdefghijklmnopqrstuvwxyzabcdefghijklmn"},
		{"nothing left of the title", 10, "Spec: ???", "smith/10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := workbench.Branch(tt.number, tt.title); got != tt.want {
				t.Errorf("Branch(%d, %q) = %q, want %q", tt.number, tt.title, got, tt.want)
			}
		})
	}
}
