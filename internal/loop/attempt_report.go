package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/byranZA/smith/internal/tracker"
)

// removeReport removes the report file an earlier attempt left, and makes
// room for the one the next writes.
func removeReport(file string) error {
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the report file %s: %w", file, err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("make room for the report file %s: %w", file, err)
	}
	return nil
}

// postReport posts the report the agent left for attempt number attempt at
// task as a comment on task's issue, under a header saying how the attempt
// ended (closed at the task branch's tip, killed, ended in an agent failure,
// or left open), the preset it ran on and how long it took. No report, or a
// failed read, tip or comment write, is a warning.
func (l Loop) postReport(ctx context.Context, task tracker.Task, attempt int, closed bool, run attempt) error {
	file := l.Bench.ReportFile(task.Number)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return l.report("smith: warning: the agent left no report for #%d at %s\n", task.Number, file)
	}
	if err != nil {
		return l.warnIf(err, "could not read the report of #%d", task.Number)
	}
	ending := "left open"
	switch {
	case run.killed():
		ending = "killed"
	case run.agentFailure():
		ending = "ended in an agent failure"
	}
	if closed {
		tip, terr := l.Bench.TaskTip(ctx, task.Number)
		if terr != nil {
			return l.warnIf(terr, "could not read the commit #%d closed at, so its report is not posted", task.Number)
		}
		ending = "closed at " + tip
	}
	body := fmt.Sprintf("smith: attempt %d of %d, %s, on %s, took %s.\n\n%s", attempt, l.Limits.MaxAttempts, ending, presetPhrase(run.preset), run.took.Round(time.Second), data)
	return l.warnIf(l.Tracker.Comment(ctx, task.Number, body), "could not comment on #%d", task.Number)
}
