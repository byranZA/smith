package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

// unknown stands in the skip comment for a value the loop could not read.
const unknown = "unknown"

// noteSkip comments on skipped task's issue with where its work was left: the
// machine, the task worktree dir, the task branch, the attempts it got in this
// run, and how far its branch got beyond the work branch. A value it cannot
// read is marked unknown, and that or a failed comment write is a warning.
func (l Loop) noteSkip(ctx context.Context, task tracker.Task, dir string, attempts int) error {
	host, err := l.hostname()
	if err := l.warnIf(err, "could not read this machine's hostname"); err != nil {
		return err
	}
	progress := unknown
	ahead, err := l.Bench.Ahead(ctx, task.Number)
	if err == nil {
		progress = progressOf(ahead, l.Bench.WorkBranch())
	}
	if err := l.warnIf(err, "could not count the commits on #%d's task branch", task.Number); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("smith skipped this task and handed it to a human: it was still open after its last attempt.\n\n")
	fmt.Fprintf(&b, "- **Machine:** %s\n", host)
	fmt.Fprintf(&b, "- **Task worktree:** `%s`\n", dir)
	fmt.Fprintf(&b, "- **Task branch:** `%s`\n", l.Bench.TaskBranch(task.Number))
	fmt.Fprintf(&b, "- **Attempts:** %d in this run\n", attempts)
	fmt.Fprintf(&b, "- **Progress:** %s\n", progress)
	return l.warnIf(l.Tracker.Comment(ctx, task.Number, b.String()), "could not comment on #%d", task.Number)
}

// hostname names the machine the loop runs on, unknown when Hostname is nil
// or fails, with the failure.
func (l Loop) hostname() (string, error) {
	if l.Hostname == nil {
		return unknown, nil
	}
	host, err := l.Hostname()
	if err != nil {
		return unknown, err
	}
	return host, nil
}

// progressOf says how many commits a task branch has beyond workBranch.
func progressOf(ahead int, workBranch string) string {
	switch ahead {
	case 0:
		return fmt.Sprintf("nothing committed beyond the work branch `%s`", workBranch)
	case 1:
		return fmt.Sprintf("1 commit beyond the work branch `%s`", workBranch)
	default:
		return fmt.Sprintf("%d commits beyond the work branch `%s`", ahead, workBranch)
	}
}
