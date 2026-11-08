package loop_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/review"
	"github.com/byranZA/smith/internal/tracker"
)

const noFindings = `{"findings": [], "not_filed": []}`

// reviewBench is a bench whose review opens a detached worktree at tip, with the findings file in a directory of its own.
type reviewBench struct {
	trees
	board *board
	file  string
	// strays is what the bench reports the review left behind in its worktree.
	strays loop.Strays
}

const (
	mergeBase = "1111111111111111111111111111111111111111"
	tip       = "2222222222222222222222222222222222222222"
)

func (r reviewBench) OpenReview(_ context.Context, issue int) (string, error) {
	r.board.events = append(r.board.events, "open review "+strings.TrimPrefix(worktreeOf(issue), "/bench/"))
	return "/bench/review", nil
}

func (reviewBench) ReviewBase(context.Context) (string, string, error) { return mergeBase, tip, nil }

func (r reviewBench) FindingsFile(int) string { return r.file }

func (r reviewBench) ReviewStrays(context.Context, string, string) (loop.Strays, error) {
	return r.strays, nil
}

// reviewer is a launcher that works tasks as the board does and, handed the review prompt, leaves written in the findings file and exits with exit.
type reviewer struct {
	board   *board
	file    string
	written string
	exit    error
	// stale is what the findings file holds before the agent starts, if anything.
	sawStale bool
}

func (r *reviewer) Launch(ctx context.Context, cmd agent.Command) error {
	prompt := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(prompt, "review step") {
		return r.board.Launch(ctx, cmd)
	}
	r.board.launched = append(r.board.launched, cmd)
	r.board.events = append(r.board.events, "launch review")
	if _, err := os.Stat(r.file); err == nil {
		r.sawStale = true
	}
	if r.written != "" {
		if err := os.WriteFile(r.file, []byte(r.written), 0o644); err != nil {
			return err
		}
	}
	return r.exit
}

// reviewingLoop returns a loop over b with the review role set to a reviewer preset, and the launcher its agents run through.
func reviewingLoop(t *testing.T, b *board, written string) (loop.Loop, *reviewer) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "smith", "review-42.json")
	choice, err := preset.Choose(repofile.File{
		Presets: map[string]repofile.Preset{"reviewer": {Agent: "codex", Model: "gpt-5"}},
		Roles:   map[string]string{"review": "reviewer"},
	}, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose() error = %v", err)
	}
	resolved, ok := choice.Review()
	if !ok {
		t.Fatal("Review() = unset, want the reviewer preset")
	}
	launcher := &reviewer{board: b, file: file, written: written}
	l := loopOn(t, b, launcher)
	l.Bench = reviewBench{board: b, file: file}
	l.Review = &loop.Review{Preset: resolved, Prompt: loop.BuiltinReviewPrompt()}
	return l, launcher
}

func TestRunReviewsOnceAfterTheLastTaskClosesAndMarksTheSpecReviewed(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start 43", "launch 43", "stop 43", "start 44", "launch 44", "stop 44", "start review 42", "open review 42", "launch review", "post review", "mark reviewed 42", "edit review", "stop review 42"}
	if err != nil || !outcome.Complete() || outcome.Ending() != loop.Complete || !slices.Equal(b.events, want) {
		t.Errorf("Run() = %+v, %v with events %q; want complete with events %q", outcome, err, b.events, want)
	}
}

func TestReviewPostsEachAxisAsNoFindingsWithItsLedger(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, noFindings)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	review := b.comments[0]
	for _, want := range []string{"## Spec\n\nNo findings.", "## Standards\n\nNo findings.", "## Quality\n\nNo findings.", "<!-- smith:review ", tip} {
		if review.number != 42 || !strings.Contains(review.body, want) {
			t.Errorf("review comment on #%d = %q, want it on #42 holding %q", review.number, review.body, want)
		}
	}
}

func TestReviewAgentRunsOnTheReviewPresetInTheReviewWorktreeWithItsPromptFilled(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	cmd := b.launched[0]
	prompt := cmd.Args[len(cmd.Args)-1]
	for _, want := range []string{"#42 — spec", "git diff " + mergeBase + "...HEAD", launcher.file} {
		if !strings.Contains(prompt, want) {
			t.Errorf("review prompt = %q, want it to hold %q", prompt, want)
		}
	}
	if strings.Contains(prompt, "{{") {
		t.Errorf("review prompt = %q, want every placeholder filled", prompt)
	}
	if cmd.Name != "codex" || !slices.Contains(cmd.Args, "gpt-5") || cmd.Dir != "/bench/review" {
		t.Errorf("review command = %+v, want codex on gpt-5 in /bench/review", cmd)
	}
}

func TestRunWithoutAReviewRoleNeverReviews(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Review = nil

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || !outcome.Complete() || !slices.Equal(b.handed(), []string{"43"}) {
		t.Errorf("Run() = %+v, %v, handed %v; want complete after task #43 alone", outcome, err, b.handed())
	}
}

func TestRunOnACompleteUnreviewedSpecOnlyReviews(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start review 42", "open review 42", "launch review", "post review", "mark reviewed 42", "edit review", "stop review 42"}
	if err != nil || !outcome.Complete() || !slices.Equal(b.events, want) {
		t.Errorf("Run() = %+v, %v with events %q; want only the review %q", outcome, err, b.events, want)
	}
}

func TestRunNeverReviewsWhatIsNotAReviewableCompleteExecution(t *testing.T) {
	tests := map[string]*board{
		"a reviewed spec":                      {tasks: []tracker.Task{closed(agentTask(43))}, reviewed: true},
		"a spec with only a human task left":   {tasks: []tracker.Task{closed(agentTask(43)), humanTask(44)}},
		"a spec with a task the agent skipped": {tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}},
		"a child task run on its own":          {tasks: []tracker.Task{agentTask(43)}, single: true},
	}
	for name, b := range tests {
		t.Run(name, func(t *testing.T) {
			l, _ := reviewingLoop(t, b, noFindings)

			if _, err := l.Run(t.Context(), 42); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if slices.Contains(b.events, "launch review") || slices.Contains(b.events, "start review 42") {
				t.Errorf("events = %q, want no review", b.events)
			}
		})
	}
}

func TestReviewMarkIsRemovedWhenTheReviewFails(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, `not json`)
	l.Limits.MaxAttempts = 2

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start review 42", "open review 42", "launch review", "open review 42", "launch review", "stop review 42"}
	if err != nil || outcome.Ending() != loop.ReviewFailed || outcome.ReviewDir != "/bench/review" || !slices.Equal(b.events, want) || len(b.comments) != 1 {
		t.Errorf("Run() = %+v, %v with events %q and comments %v; want review failed after two attempts, no mark and only the stop report posted", outcome, err, b.events, b.comments)
	}
	if !strings.Contains(outcome.Report("spec #42", l.Limits), "/bench/review") || launcher.written != "not json" {
		t.Errorf("Report() = %q, want it to say where the review worktree is", outcome.Report("spec #42", l.Limits))
	}
}

func TestReviewFailsWhenTheAgentLeavesNoFindingsFile(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, "")

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.ReviewFailed || !slices.Contains(b.events, "stop review 42") || slices.Contains(b.events, "mark reviewed 42") {
		t.Errorf("Run() = %+v, %v with events %q; want review failed and not marked reviewed", outcome, err, b.events)
	}
}

func TestReviewRemovesAStaleFindingsFileBeforeTheAttempt(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	if err := os.MkdirAll(filepath.Dir(launcher.file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher.file, []byte(`{"findings": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if launcher.sawStale {
		t.Error("the findings file was still there when the review agent started, want it removed")
	}
}

func TestReviewAttemptCountsAgainstTheCap(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Limits.MaxIterations = 1

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.CapReached || slices.Contains(b.events, "launch review") || slices.Contains(b.events, "start review 42") {
		t.Errorf("Run() = %+v, %v with events %q; want cap reached with the review deferred", outcome, err, b.events)
	}
}

func TestRunWithNoCapReviewsInTheSameRunAfterElevenTasks(t *testing.T) {
	var tasks []tracker.Task
	for n := 43; n < 54; n++ {
		tasks = append(tasks, agentTask(n))
	}
	b := &board{tasks: tasks}
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.Complete || !slices.Contains(b.events, "launch review") {
		t.Errorf("Run() = %+v, %v with events %q; want complete with the review run", outcome, err, b.events)
	}
}

func TestReviewGetsAsManyAttemptsAsTheLimitAllows(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, "")
	l.Limits.MaxAttempts = 3

	outcome, err := l.Run(t.Context(), 42)

	launches := 0
	for _, e := range b.events {
		if e == "launch review" {
			launches++
		}
	}
	if err != nil || outcome.Ending() != loop.ReviewFailed || launches != 3 {
		t.Errorf("Run() = %+v, %v with %d review launches; want review failed after 3", outcome, err, launches)
	}
}

func TestReviewEveryAttemptAnAgentFailureEndsAgentBroken(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	l.Limits.MaxAttempts = 2
	launcher.exit = errors.New("exit status 1")

	outcome, err := l.Run(t.Context(), 42)

	launches := 0
	for _, e := range b.events {
		if e == "launch review" {
			launches++
		}
	}
	if err != nil || outcome.Ending() != loop.AgentBroken || launches != 2 || slices.Contains(b.events, "mark reviewed 42") || !slices.Contains(b.events, "stop review 42") {
		t.Errorf("Run() = %+v, %v with events %q; want agent broken after two attempts, no marks", outcome, err, b.events)
	}
	if got := outcome.Report("spec #42", l.Limits); !strings.Contains(got, "the review of #42") || !strings.Contains(got, "exit status 1") {
		t.Errorf("Report() = %q, want it to name the review and the agent's error", got)
	}
}

func TestReviewAgentFailingOnlyOnceIsReviewFailedNotBroken(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, "")
	l.Limits.MaxAttempts = 2
	l.Launcher = &flaky{reviewer: launcher}

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.ReviewFailed {
		t.Errorf("Run() = %+v, %v; want review failed, one attempt being a clean exit without a file", outcome, err)
	}
}

// flaky is a launcher whose review agent fails its first launch and exits cleanly after.
type flaky struct {
	*reviewer
	launches int
}

func (f *flaky) Launch(ctx context.Context, cmd agent.Command) error {
	f.launches++
	f.exit = nil
	if f.launches == 1 {
		f.exit = errors.New("exit status 1")
	}
	return f.reviewer.Launch(ctx, cmd)
}

func TestReviewAgentThatCannotStartEndsAgentBrokenWithoutCountingAnAttempt(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	launcher.exit = fmt.Errorf("run codex: %w: not found", loop.ErrNotStarted)

	outcome, err := l.Run(t.Context(), 42)

	launches := slices.DeleteFunc(slices.Clone(b.events), func(e string) bool { return e != "launch review" })
	if err != nil || outcome.Ending() != loop.AgentBroken || len(launches) != 1 || !strings.Contains(outcome.Report("spec #42", l.Limits), "could not be started") {
		t.Errorf("Run() = %+v, %v with events %q; want agent broken on the first launch", outcome, err, b.events)
	}
}

func TestReviewThatCommitsOrChangesFilesStopsAtOnceKeepingTheWorktree(t *testing.T) {
	tests := []struct {
		name   string
		strays loop.Strays
		want   string
	}{
		{"commit", loop.Strays{Commits: []string{"abc1234 sneaky fix"}}, "abc1234 sneaky fix"},
		{"changed file", loop.Strays{Changed: []string{"internal/x.go"}}, "internal/x.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
			l, launcher := reviewingLoop(t, b, noFindings)
			l.Limits.MaxAttempts = 3
			l.Bench = reviewBench{board: b, file: launcher.file, strays: tt.strays}

			outcome, err := l.Run(t.Context(), 42)

			report := outcome.Report("spec #42", l.Limits)
			if err != nil || outcome.Ending() != loop.ReviewFailed || !strings.Contains(report, tt.want) || !strings.Contains(report, "/bench/review") {
				t.Errorf("Run() = %+v, %v with report %q; want review failed naming %q and the worktree", outcome, err, report, tt.want)
			}
			if slices.Contains(b.events, "mark reviewed 42") || !slices.Contains(b.events, "stop review 42") || len(b.filed) != 0 || len(b.comments) != 1 {
				t.Errorf("events = %q, filed %v, comments %v; want no marks, no filing and only the stop report", b.events, b.filed, b.comments)
			}
			if got := slices.DeleteFunc(slices.Clone(b.events), func(e string) bool { return e != "launch review" }); len(got) != 1 {
				t.Errorf("review launched %d times, want 1 with no retry", len(got))
			}
		})
	}
}

func TestReviewAFailedStartMarkIsAWarningAndTheReviewStillRuns(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}, refuseStartReview: errors.New("HTTP 403")}
	l, _ := reviewingLoop(t, b, noFindings)
	var progress strings.Builder
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not mark #42 as under review: HTTP 403"}
	if err != nil || !outcome.Complete() || !containsLines(progress.String(), want) || !slices.Contains(b.events, "launch review") {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant the warning and the review run", outcome, err, progress.String())
	}
}

func TestReviewInvalidFilesFailTheAttemptWithTheirError(t *testing.T) {
	good := finding("quality", "minor", "agent", "Q one")
	tests := map[string]string{
		"not json":              `not json`,
		"unknown axis":          findingsFile("", strings.Replace(good, "quality", "style", 1)),
		"unknown severity":      findingsFile("", strings.Replace(good, "minor", "trivial", 1)),
		"unknown who":           findingsFile("", strings.Replace(good, `"agent"`, `"robot"`, 1)),
		"empty title":           findingsFile("", strings.Replace(good, `"Q one"`, `""`, 1)),
		"empty body":            findingsFile("", strings.Replace(good, `"Q one body"`, `""`, 1)),
		"blocked by a stranger": findingsFile("", finding("quality", "minor", "agent", "Q one", 7)),
		"blocked by itself":     findingsFile("", finding("quality", "minor", "agent", "Q one", 1)),
	}
	for name, written := range tests {
		t.Run(name, func(t *testing.T) {
			b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
			l, _ := reviewingLoop(t, b, written)
			l.Limits.MaxAttempts = 1
			var progress strings.Builder
			l.Progress = &progress

			outcome, err := l.Run(t.Context(), 42)

			if err != nil || outcome.Ending() != loop.ReviewFailed || !strings.Contains(progress.String(), "the review of #42 failed: read the findings file") || len(b.filed) != 0 || len(b.comments) != 1 || slices.Contains(b.events, "post review") {
				t.Errorf("Run() = %+v, %v with progress %q, events %q; want a failed attempt reporting its validation error and nothing filed", outcome, err, progress.String(), b.events)
			}
		})
	}
}

func TestRunClearsAStaleReviewingMarkBeforeAnyAgentRuns(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}, reviewing: true}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Review = nil

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.events) < 2 || b.events[0] != "stop review 42" {
		t.Errorf("events = %q, want the stale review mark removed first", b.events)
	}
}

// finding renders one finding of a findings file.
func finding(axis, severity, who, title string, blockedBy ...int) string {
	blocked := ""
	if len(blockedBy) > 0 {
		blocked = `, "blocked_by": [` + strings.Trim(strings.Join(strings.Fields(strings.Trim(fmt.Sprint(blockedBy), "[]")), ", "), " ") + `]`
	}
	return `{"axis": "` + axis + `", "severity": "` + severity + `", "title": "` + title + `", "who": "` + who + `", "body": "` + title + ` body"` + blocked + `}`
}

func findingsFile(not string, findings ...string) string {
	return `{"findings": [` + strings.Join(findings, ", ") + `], "not_filed": [` + not + `]}`
}

func TestReviewFilesEachFindingAsATaskInSpecOrderThenWorksTheAgentOnes(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile("",
		finding("quality", "minor", "agent", "Q one"),
		finding("standards", "major", "human", "St one"),
		finding("spec", "blocker", "agent", "S one"),
	))

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start review 42", "open review 42", "launch review", "post review", "file S one", "file St one", "file Q one", "edit review", "mark reviewed 42", "edit review", "stop review 42"}
	if err != nil || !slices.Equal(b.events[:len(want)], want) {
		t.Fatalf("Run() = %+v, %v with events %q; want it to start with %q", outcome, err, b.events, want)
	}
	if got := b.filed[0].For; got != tracker.Agent || b.filed[1].For != tracker.Human || b.filed[2].For != tracker.Agent {
		t.Errorf("filed audiences = %v, %v, %v; want agent, human, agent", got, b.filed[1].For, b.filed[2].For)
	}
	if body := b.filed[0].Body; body != "S one body\n\nSource: Spec axis, blocker, finding S1, review of #42\n" {
		t.Errorf("filed body = %q, want the finding's body and its Source line", body)
	}
	if handed := b.handed(); !slices.Equal(handed[len(handed)-2:], []string{"101", "103"}) {
		t.Errorf("handed %v, want the agent tasks #101 and #103 worked last", handed)
	}
	if got := b.comments[0].body; !strings.Contains(got, "S one (#101)") || !strings.Contains(got, "Q one (#103)") {
		t.Errorf("review comment = %q, want the filed tasks named", got)
	}
}

func TestReviewRunEndsWaitingOnAHumanWhenItFiledOnlyHumanTasks(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile("", finding("spec", "major", "human", "S one")))

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.WaitingOnHuman || strings.Count(strings.Join(b.events, "|"), "launch review") != 1 {
		t.Errorf("Run() = %+v, %v with events %q; want waiting on human after one review", outcome, err, b.events)
	}
}

func TestReviewRunEndsCompleteWithoutASecondReviewAndNamesTheFiledTasks(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile("", finding("spec", "major", "agent", "S one"), finding("quality", "minor", "agent", "Q one")))

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.Complete || strings.Count(strings.Join(b.events, "|"), "launch review") != 1 {
		t.Fatalf("Run() = %+v, %v with events %q; want complete after one review", outcome, err, b.events)
	}
	report := outcome.Report("spec #42", l.Limits)
	stop := b.comments[len(b.comments)-1].body
	for _, got := range []string{report, stop} {
		if !strings.Contains(got, "#101 (S1)") || !strings.Contains(got, "#102 (Q1)") {
			t.Errorf("report = %q, want it to name the filed tasks #101 and #102", got)
		}
	}
}

func TestReviewLinksAFindingToTheTaskItIsBlockedBy(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile("",
		finding("quality", "minor", "agent", "Q two", 2),
		finding("quality", "minor", "agent", "Q one"),
	))

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if b.filed[0].Title != "Q one" || !slices.Equal(b.filed[1].BlockedBy, []int{101}) {
		t.Errorf("filed %+v, want Q one first and Q two blocked by #101", b.filed)
	}
}

func TestReviewNeverFilesARestructuringTooLargeForATask(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile(`{"title": "Split the loop", "summary": "Too big."}`, finding("spec", "major", "agent", "S one")))

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.filed) != 1 || !strings.Contains(b.comments[0].body, "**Not filed**") || !strings.Contains(b.comments[0].body, "Split the loop") {
		t.Errorf("filed %+v, comment %q; want one task and the restructuring under Not filed", b.filed, b.comments[0].body)
	}
}

func TestReviewStopsAtOnceWhenFilingFailsEndingIncompleteWithTheLedgerPosted(t *testing.T) {
	second := 1
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}, refuseFile: &second}
	l, _ := reviewingLoop(t, b, findingsFile("", finding("spec", "major", "agent", "S one"), finding("spec", "minor", "agent", "S two"), finding("quality", "minor", "agent", "Q one")))

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start review 42", "open review 42", "launch review", "post review", "file S one", "stop review 42"}
	if err != nil || outcome.Ending() != loop.ReviewIncomplete || !slices.Equal(b.events, want) {
		t.Fatalf("Run() = %+v, %v with events %q; want review incomplete with events %q", outcome, err, b.events, want)
	}
	report := outcome.Report("spec #42", l.Limits)
	if !strings.Contains(report, "ledger is posted and 1 of 3 findings are filed") {
		t.Errorf("Report() = %q, want the ledger posted and 1 of 3 filed", report)
	}
}

// ledgerBoard is a board whose spec carries the ledger of a review of three findings made at sha, with the first of them (S1) filed as #101.
func ledgerBoard(t *testing.T, sha string, filedFirst bool) *board {
	t.Helper()
	file, err := review.Parse([]byte(findingsFile("",
		finding("spec", "major", "agent", "S one"),
		finding("spec", "minor", "agent", "S two"),
		finding("quality", "minor", "human", "Q one"),
	)))
	if err != nil {
		t.Fatal(err)
	}
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	b.existing = []tracker.Comment{{ID: 7, Body: "chatter"}, {ID: 8, Body: review.Comment(file, 42, sha, nil)}}
	b.comments = make([]posted, 9)
	if filedFirst {
		b.filed = []tracker.NewTask{{}}
		b.tasks = append(b.tasks, tracker.Task{Number: 101, Title: "S one", Open: false, For: tracker.Agent, Body: review.TaskBody(file.Findings[0], "S1", 42)})
	}
	return b
}

func TestRunFilesOnlyTheMissingFindingsOfALedgerWithoutReviewingAgain(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"start review 42", "file S two", "file Q one", "edit review", "mark reviewed 42", "edit review", "stop review 42"}
	if err != nil || !slices.Equal(b.events[:len(want)], want) {
		t.Fatalf("Run() = %+v, %v with events %q; want it to start with %q", outcome, err, b.events, want)
	}
	if slices.Contains(b.events, "launch review") || len(b.filed) != 3 {
		t.Errorf("events %q, filed %d; want no review agent and two more tasks filed", b.events, len(b.filed))
	}
	if got := b.comments[8].body; !strings.Contains(got, "S one (#101)") || !strings.Contains(got, "S two (#102)") || !strings.Contains(got, "Q one (#103)") {
		t.Errorf("ledger comment = %q, want all three tasks named", got)
	}
	if outcome.Ending() == loop.ReviewIncomplete || outcome.Note != "" {
		t.Errorf("Run() = %+v, want a resumed review with no note", outcome)
	}
}

func TestResumeReportsEveryFiledTaskIncludingEarlierOnes(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	report := outcome.Report("spec #42", l.Limits)
	if err != nil || !strings.Contains(report, "review filed: #101 (S1), #102 (S2), #103 (Q1)") {
		t.Errorf("Run() = %v, report %q; want all three tasks named in filing order", err, report)
	}
}

func TestEditOnlyRetryReportsEveryPreviouslyFiledTask(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	b.refuseEdit = errors.New("HTTP 502")
	l, _ := reviewingLoop(t, b, noFindings)
	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	b.refuseEdit = nil

	outcome, err := l.Run(t.Context(), 42)

	report := outcome.Report("spec #42", l.Limits)
	if err != nil || !strings.Contains(report, "review filed: #101 (S1), #102 (S2), #103 (Q1)") {
		t.Errorf("Run() = %v, report %q; want all three tasks named", err, report)
	}
}

func TestResumeNotesThatTheWorkBranchMoved(t *testing.T) {
	b := ledgerBoard(t, "9999999999999999999999999999999999999999", true)
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || len(b.filed) != 3 || !slices.Contains(b.events, "mark reviewed 42") {
		t.Fatalf("Run() = %+v, %v with events %q; want the missing findings filed anyway", outcome, err, b.events)
	}
	if report := outcome.Report("spec #42", l.Limits); !strings.Contains(report, "work branch moved") || !strings.Contains(report, tip) {
		t.Errorf("Report() = %q, want it to note the branch moved to %s", report, tip)
	}
}

func TestRunStopsBeforeAnyAgentWhenTheLedgerCannotBeParsed(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}, existing: []tracker.Comment{{ID: 8, Body: "review\n<!-- smith:review {nope -->\n"}}}
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err == nil || !strings.Contains(err.Error(), "comment 8") || len(b.launched) != 0 {
		t.Errorf("Run() = %+v, %v with launches %d; want an error naming comment 8 and no agent run", outcome, err, len(b.launched))
	}
}

func TestFailedEditAfterEveryTaskIsFiledEndsIncompleteAndTheNextRunOnlyEditsAndMarks(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	b.refuseEdit = errors.New("HTTP 502")
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.ReviewIncomplete || slices.Contains(b.events, "mark reviewed 42") || !slices.Contains(b.events, "stop review 42") {
		t.Fatalf("Run() = %+v, %v with events %q; want review incomplete, no reviewed mark and the review mark removed", outcome, err, b.events)
	}
	if report := outcome.Report("spec #42", l.Limits); !strings.Contains(report, "3 of 3 findings are filed") {
		t.Errorf("Report() = %q, want 3 of 3 filed", report)
	}

	b.refuseEdit, b.events = nil, nil
	filedBefore := len(b.filed)
	outcome, err = l.Run(t.Context(), 42)

	want := []string{"start review 42", "edit review", "mark reviewed 42", "edit review", "stop review 42"}
	if err != nil || len(b.filed) != filedBefore || !slices.Equal(b.events[:len(want)], want) {
		t.Errorf("second Run() = %+v, %v with events %q, filed %d; want only an edit and a mark", outcome, err, b.events, len(b.filed))
	}
}

func TestFailedMarkEndsReviewIncomplete(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	b.refuseMark = errors.New("HTTP 502")
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.ReviewIncomplete {
		t.Errorf("Run() = %+v, %v; want review incomplete", outcome, err)
	}
}

func hintOf(report string) string {
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "smith loop run") {
			return line
		}
	}
	return ""
}

func TestChildTaskRunAloneHintsAtReviewingTheSpecItCompleted(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(80)}, single: true, parent: 42, siblings: []tracker.Task{closed(agentTask(81))}}
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 80)

	report := outcome.Report("task #80", l.Limits)
	if err != nil || !outcome.Complete() || slices.Contains(b.events, "launch review") || !strings.HasSuffix(report, "`smith loop run 42` reviews it\n") || hintOf(report) == "" {
		t.Errorf("Run() = %+v, %v, report %q; want complete, no review and a hint ending the report", outcome, err, report)
	}
}

func TestChildTaskRunAloneHasNoHintUnlessTheSpecIsCompleteUnreviewedAndReviewIsSet(t *testing.T) {
	tests := map[string]struct {
		siblings []tracker.Task
		reviewed bool
		unset    bool
	}{
		"the spec still has open tasks": {siblings: []tracker.Task{agentTask(81)}},
		"the spec is reviewed":          {siblings: []tracker.Task{closed(agentTask(81))}, reviewed: true},
		"the review role is unset":      {siblings: []tracker.Task{closed(agentTask(81))}, unset: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			b := &board{tasks: []tracker.Task{agentTask(80)}, single: true, parent: 42, siblings: tc.siblings, reviewed: tc.reviewed}
			l, _ := reviewingLoop(t, b, noFindings)
			if tc.unset {
				l.Review = nil
			}

			outcome, err := l.Run(t.Context(), 80)

			if report := outcome.Report("task #80", l.Limits); err != nil || hintOf(report) != "" {
				t.Errorf("Run() error = %v, report %q; want no hint", err, report)
			}
		})
	}
}

func TestStandaloneTaskIsReviewedAndTheTasksItFilesAreWorkedUnderItClosed(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(90)}, single: true, standalone: true}
	l, _ := reviewingLoop(t, b, findingsFile("", finding("spec", "major", "agent", "S one")))

	outcome, err := l.Run(t.Context(), 90)

	if err != nil || !outcome.Complete() || !slices.Contains(b.events, "launch 101") || b.tasks[0].Open || b.events[len(b.events)-1] != "stop 101" {
		t.Errorf("Run() = %+v, %v with events %q; want the filed #101 worked after the review and #90 still closed", outcome, err, b.events)
	}
}

func TestRunWithNoReviewSkipsTheReviewLeavesNoMarkAndSaysSo(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Review, l.NoReview = nil, true

	outcome, err := l.Run(t.Context(), 42)

	report := outcome.Report("spec #42", loop.Limits{})
	if err != nil || outcome.Ending() != loop.Complete || !strings.Contains(report, "not reviewed: `--no-review`") {
		t.Errorf("Run() = %+v, %v; want complete with report %q naming --no-review", outcome, err, report)
	}
	for _, event := range b.events {
		if strings.Contains(event, "review") {
			t.Errorf("events = %q, want no review step or mark", b.events)
		}
	}
}

func TestRunWithNoReviewOnACompleteUnreviewedSpecHasNothingToDo(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Review, l.NoReview = nil, true

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.Complete || len(b.events) != 0 || len(b.handed()) != 0 {
		t.Errorf("Run() = %+v, %v with events %q; want complete and nothing done", outcome, err, b.events)
	}
}

func TestRunWithNoReviewSaysNothingOfAReviewForAnExecutionThatNeverReviews(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}, single: true}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Review, l.NoReview = nil, true

	outcome, err := l.Run(t.Context(), 42)

	if report := outcome.Report("task #43", loop.Limits{}); err != nil || strings.Contains(report, "not reviewed") {
		t.Errorf("Run() = %v, report %q; want no not-reviewed line for a child task", err, report)
	}
}

func TestRemovingTheReviewedMarkAfterACompletedReviewStartsAFreshReview(t *testing.T) {
	b := ledgerBoard(t, tip, true)
	file, err := review.Parse([]byte(findingsFile("", finding("spec", "major", "agent", "S one"))))
	if err != nil {
		t.Fatal(err)
	}
	b.existing[1].Body = review.CommentDone(file, 42, tip, map[int]int{0: 101})
	l, _ := reviewingLoop(t, b, noFindings)

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || !slices.Contains(b.events, "launch review") || !slices.Contains(b.events, "mark reviewed 42") {
		t.Errorf("Run() = %+v, %v with events %q; want a fresh review agent run and the issue marked reviewed", outcome, err, b.events)
	}
}

func TestFinishedFilingMarksTheReviewCommentDoneAfterTheIssueIsMarked(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, findingsFile("", finding("spec", "major", "agent", "S one")))

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatal(err)
	}

	want := []string{"mark reviewed 42", "edit review", "stop review 42"}
	i := slices.Index(b.events, "mark reviewed 42")
	if i < 0 || !slices.Equal(b.events[i:i+len(want)], want) {
		t.Errorf("events %q; want the mark followed by a final comment edit", b.events)
	}
	var got review.Ledger
	for _, c := range b.comments {
		if l, ok, err := review.ParseLedger(c.body); err == nil && ok {
			got = l
		}
	}
	if !got.Done {
		t.Errorf("ledger = %+v; want it done", got)
	}
}
