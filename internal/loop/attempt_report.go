package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

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

// postReport posts the report the agent left for the run named by who, such as
// "attempt 1 of 2" or "interactive run", at task as a comment on task's issue, under a header saying how the attempt
// ended (closed at the task branch's tip, killed, ended in an agent failure,
// or left open), the preset it ran on and how long it took. No report, or a
// failed read, tip or comment write, is a warning.
func (l Loop) postReport(ctx context.Context, task tracker.Task, who string, closed bool, run attempt) error {
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
	header := fmt.Sprintf("smith: %s, %s, on %s, took %s.\n\n", who, ending, presetPhrase(run.preset), run.took.Round(time.Second))
	body := header + tailWithin(string(data), commentCap-utf8.RuneCountInString(header))
	return l.warnIf(l.Tracker.Comment(ctx, task.Number, body), "could not comment on #%d", task.Number)
}

// commentCap is the most characters a report comment, header included, may
// hold: under GitHub's 65,536-character limit.
const commentCap = 60000

// cutMarker leads a report that was cut to its tail.
const cutMarker = "(earlier text was cut)\n\n"

// tailWithin returns report whole when it fits in room characters, otherwise
// its tail behind cutMarker, the marker counted in room.
func tailWithin(report string, room int) string {
	if utf8.RuneCountInString(report) <= room {
		return report
	}
	keep := max(room-utf8.RuneCountInString(cutMarker), 0)
	runes := []rune(report)
	return cutMarker + string(runes[len(runes)-keep:])
}
