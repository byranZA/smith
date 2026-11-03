package loop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

const noFindings = `{"findings": [], "not_filed": []}`

// reviewBench is a bench whose review opens a detached worktree at tip, with the findings file in a directory of its own.
type reviewBench struct {
	trees
	board *board
	file  string
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

	want := []string{"start 43", "launch 43", "stop 43", "start 44", "launch 44", "stop 44", "start review 42", "open review 42", "launch review", "mark reviewed 42", "stop review 42"}
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

	want := []string{"start review 42", "open review 42", "launch review", "mark reviewed 42", "stop review 42"}
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

func TestReviewEveryAttemptAnAgentFailureEndsReviewFailed(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	launcher.exit = errors.New("exit status 1")

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Ending() != loop.ReviewFailed || slices.Contains(b.events, "mark reviewed 42") {
		t.Errorf("Run() = %+v, %v; want review failed and not marked reviewed", outcome, err)
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
