package loop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// reporter is a journal whose agent writes writes to the report file, after
// checking that the file was removed first, and which logs each comment.
type reporter struct {
	*journal
	file string
	// writes is what the agent leaves in the report file; empty leaves none.
	writes string
	// leftover logs a report file still present when the agent launched.
	leftover []string
	// fails is the error Launch returns after the report is written.
	fails error
}

func (r *reporter) ReportFile(int) string { return r.file }

func (r *reporter) TaskTip(context.Context, int) (string, error) { return "abc1234", nil }

func (r *reporter) Launch(ctx context.Context, cmd agent.Command) error {
	if _, err := os.Stat(r.file); err == nil {
		r.leftover = append(r.leftover, "report file present at launch")
	}
	if r.writes != "" {
		if err := os.WriteFile(r.file, []byte(r.writes), 0o644); err != nil {
			return err
		}
	}
	return errors.Join(r.journal.Launch(ctx, cmd), r.fails)
}

// logging logs each comment into the journal's events.
type logging struct {
	*board
	events *[]string
}

func (l logging) Comment(ctx context.Context, number int, body string) error {
	*l.events = append(*l.events, "comment "+strings.SplitN(body, "\n", 2)[0])
	return l.board.Comment(ctx, number, body)
}

// stepClock is a clock that moves 90 seconds on each reading.
type stepClock struct{ at time.Time }

func (c *stepClock) Now() time.Time {
	c.at = c.at.Add(90 * time.Second)
	return c.at
}

func reportingLoop(t *testing.T, r *reporter) loop.Loop {
	t.Helper()
	l := journalLoop(t, r.journal)
	l.Bench = r
	l.Launcher = r
	l.Tracker = logging{board: r.board, events: &r.events}
	l.Clock = (&stepClock{}).Now
	return l
}

func newReporter(t *testing.T, b *board, writes string) *reporter {
	t.Helper()
	return &reporter{journal: &journal{board: b}, file: filepath.Join(t.TempDir(), "smith", "report-43.md"), writes: writes}
}

func TestAClosedAttemptsReportIsPostedBeforeItsWorkIsPushed(t *testing.T) {
	t.Parallel()
	r := newReporter(t, &board{tasks: []tracker.Task{agentTask(43)}}, "## Done\nIt works.\n")

	if _, err := reportingLoop(t, r).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"open 43", "hand 43 in /bench/43", "comment smith: attempt 1 of 2, closed at abc1234, on preset default, took 1m30s.", "land 43", "push smith/42-spec", "finish", "comment smith stopped its run of this spec: **complete**."}
	if !slices.Equal(r.events, want) {
		t.Errorf("events = %q, want %q", r.events, want)
	}
	if got, want := r.board.comments[0].body, "smith: attempt 1 of 2, closed at abc1234, on preset default, took 1m30s.\n\n## Done\nIt works.\n"; got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
}

func TestAnOpenAttemptsReportIsPostedBeforeTheSkipComment(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Left open\nStuck.\n")
	l := reportingLoop(t, r)
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.comments) < 2 || !strings.HasPrefix(b.comments[0].body, "smith: attempt 1 of 1, left open, on preset default, took 1m30s.\n\n## Left open") || !strings.HasPrefix(b.comments[1].body, "smith skipped this task") {
		t.Errorf("comments = %+v, want the report, then the skip comment", b.comments)
	}
}

func TestAnEarlierAttemptsReportIsNotPostedAgain(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "")
	if err := os.MkdirAll(filepath.Dir(r.file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.file, []byte("old report"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := reportingLoop(t, r).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for _, c := range b.comments {
		if strings.Contains(c.body, "old report") {
			t.Errorf("comment %q posts an earlier attempt's report", c.body)
		}
	}
	if len(r.leftover) != 0 {
		t.Errorf("%v", r.leftover)
	}
}

func TestAnAttemptWithNoReportGetsNoCommentAndAWarning(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	r := newReporter(t, b, "")
	var progress strings.Builder
	l := reportingLoop(t, r)
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(reportsOf(b)) != 0 || !strings.Contains(progress.String(), "smith: warning: the agent left no report for #43 at "+r.file) {
		t.Errorf("comments = %+v, progress = %q, want no comment and a warning", b.comments, progress.String())
	}
}

func TestAFailedReportCommentWarnsAndTheRunCarriesOn(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, refuseComment: os.ErrPermission}
	r := newReporter(t, b, "## Done\n")
	var progress strings.Builder
	l := reportingLoop(t, r)
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || !outcome.Complete() || !strings.Contains(progress.String(), "smith: warning: could not comment on #43: ") {
		t.Errorf("Run() = %+v, %v, progress %q; want a warning and a complete run", outcome, err, progress.String())
	}
}

func TestAnInterruptedAttemptPostsNoReport(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	r := newReporter(t, b, "## Done\n")
	ctx, cancel := context.WithCancel(t.Context())
	l := reportingLoop(t, r)
	l.Launcher = cancelling{reporter: r, cancel: cancel}

	if _, err := l.Run(ctx, 42); err == nil {
		t.Fatal("Run() error = nil, want the interruption")
	}

	if len(reportsOf(b)) != 0 {
		t.Errorf("comments = %+v, want no report", b.comments)
	}
}

// cancelling is a launcher whose run is interrupted after the agent writes its report.
type cancelling struct {
	*reporter
	cancel context.CancelFunc
}

func (c cancelling) Launch(ctx context.Context, cmd agent.Command) error {
	err := c.reporter.Launch(ctx, cmd)
	c.cancel()
	return err
}

func TestAPromptMayUseTheReportPlaceholderOrLeaveItOut(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Write {{REPORT_FILE}}.", "No report."} {
		if _, err := loop.ParsePrompt("prompt.md", []byte(text)); err != nil {
			t.Errorf("ParsePrompt(%q) error = %v, want none", text, err)
		}
	}
}

func TestRenderFillsTheReportFile(t *testing.T) {
	t.Parallel()
	got := loop.Prompt("Write {{REPORT_FILE}}.").Render(tracker.Task{Number: 43}, "/repo/.git/smith/report-43.md")
	if want := "Write /repo/.git/smith/report-43.md."; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

// reportsOf returns the attempt reports posted on b, leaving out the loop's own stop report.
func reportsOf(b *board) []posted {
	var reports []posted
	for _, c := range b.comments {
		if strings.HasPrefix(c.body, "smith: attempt") {
			reports = append(reports, c)
		}
	}
	return reports
}

func TestAKilledAttemptsReportHeaderSaysItWasKilled(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Stuck\n")
	r.fails = errKilled
	l := reportingLoop(t, r)
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: attempt 1 of 1, killed, on preset default, took 1m30s.\n\n## Stuck\n"; b.comments[0].body != want {
		t.Errorf("comment = %q, want %q", b.comments[0].body, want)
	}
}

func TestAFailedAttemptsReportHeaderSaysItEndedInAnAgentFailure(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Stuck\n")
	r.fails = errors.New("exit status 1")
	l := reportingLoop(t, r)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: attempt 1 of 2, ended in an agent failure, on preset default, took 1m30s.\n\n## Stuck\n"; b.comments[0].body != want {
		t.Errorf("comment = %q, want %q", b.comments[0].body, want)
	}
}

func TestAReportOverTheCapIsPostedAsItsTailBehindACutMarker(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "START"+strings.Repeat("é", 70000)+"END")
	l := reportingLoop(t, r)
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	body := b.comments[0].body
	if n := utf8.RuneCountInString(body); n > 60000 {
		t.Errorf("comment has %d characters, want at most 60000", n)
	}
	header := "smith: attempt 1 of 1, left open, on preset default, took 1m30s.\n\n(earlier text was cut)\n\n"
	if !strings.HasPrefix(body, header+"é") {
		t.Errorf("comment starts %q, want header and cut marker then the tail", body[:120])
	}
	if !strings.HasSuffix(body, "éEND") || strings.Contains(body, "START") {
		t.Errorf("comment does not keep the tail and drop the start")
	}
}

func TestAReportUnderTheCapIsPostedWholeWithNoMarker(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, strings.Repeat("é", 50000)+"END")
	l := reportingLoop(t, r)
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	body := b.comments[0].body
	if strings.Contains(body, "cut") || !strings.HasSuffix(body, "é"+"END") || utf8.RuneCountInString(body) < 50003 {
		t.Errorf("comment was cut or marked")
	}
}

func TestAnInteractiveRunsReportIsPostedUnderAnInteractiveRunHeader(t *testing.T) {
	t.Parallel()
	r := newReporter(t, &board{tasks: []tracker.Task{agentTask(43)}}, "## Done\nIt works.\n")

	if _, err := reportingLoop(t, r).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if got, want := r.board.comments[0].body, "smith: interactive run, closed at abc1234, on preset default, took 1m30s.\n\n## Done\nIt works.\n"; got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
}

func TestAnInteractiveRunWithNoReportGetsNoCommentAndAWarning(t *testing.T) {
	t.Parallel()
	r := newReporter(t, &board{tasks: []tracker.Task{agentTask(43)}}, "")
	var warned strings.Builder
	l := reportingLoop(t, r)
	l.Progress = &warned

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if len(r.board.comments) != 0 || !strings.Contains(warned.String(), "the agent left no report for #43") {
		t.Errorf("comments = %+v, output = %q, want no comment and a no-report warning", r.board.comments, warned.String())
	}
}

func TestTheFinalAgentFailureAttemptsReportIsPosted(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Stuck\n")
	r.fails = errors.New("exit status 1")
	l := reportingLoop(t, r)
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: attempt 1 of 1, ended in an agent failure, on preset default, took 1m30s.\n\n## Stuck\n"; len(commentsOn(b, 43)) < 1 || commentsOn(b, 43)[0] != want {
		t.Errorf("comments on #43 = %q, want first %q", commentsOn(b, 43), want)
	}
}

func TestEveryAttemptsReportAcrossConsecutiveAgentFailuresIsPosted(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Stuck\n")
	r.fails = errors.New("exit status 1")
	l := reportingLoop(t, r)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{
		"smith: attempt 1 of 2, ended in an agent failure, on preset default, took 1m30s.\n\n## Stuck\n",
		"smith: attempt 2 of 2, ended in an agent failure, on preset default, took 1m30s.\n\n## Stuck\n",
	}
	if got := commentsOn(b, 43); !reflect.DeepEqual(got, want) {
		t.Errorf("comments on #43 = %q, want %q", got, want)
	}
}

func TestComposeReportHeadsTheReportWithHowTheAttemptEnded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		facts  loop.ReportFacts
		report string
		want   string
	}{
		{"closed with a sha", loop.ReportFacts{Who: "attempt 1 of 2", Ending: loop.EndedClosed, Tip: "abc1234", Preset: "preset opus", Took: 90 * time.Second}, "## Done\n", "smith: attempt 1 of 2, closed at abc1234, on preset opus, took 1m30s.\n\n## Done\n"},
		{"closed interactive", loop.ReportFacts{Who: "interactive run", Ending: loop.EndedClosed, Tip: "f00d", Preset: "a one-off preset", Took: 3 * time.Second}, "ok", "smith: interactive run, closed at f00d, on a one-off preset, took 3s.\n\nok"},
		{"left open", loop.ReportFacts{Who: "attempt 2 of 3", Ending: loop.EndedOpen, Preset: "preset opus", Took: time.Minute}, "Stuck", "smith: attempt 2 of 3, left open, on preset opus, took 1m0s.\n\nStuck"},
		{"killed", loop.ReportFacts{Who: "attempt 1 of 1", Ending: loop.EndedKilled, Preset: "preset opus", Took: time.Minute}, "x", "smith: attempt 1 of 1, killed, on preset opus, took 1m0s.\n\nx"},
		{"agent failure interactive", loop.ReportFacts{Who: "interactive run", Ending: loop.EndedAgentFailure, Preset: "preset opus", Took: time.Minute}, "x", "smith: interactive run, ended in an agent failure, on preset opus, took 1m0s.\n\nx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := loop.ComposeReport(tt.facts, tt.report); got != tt.want {
				t.Errorf("ComposeReport() = %q, want %q", got, tt.want)
			}
		})
	}
}
