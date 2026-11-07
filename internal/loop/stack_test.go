package loop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/tracker"
)

const declaredStack = "stack:\n  up: make up\n  down: make down\n"

// stackCommands fakes the process boundary a real stack reaches its commands
// through, logging "up <dir>" and "down <dir>" into events, writing a line of
// output for each and failing the commands named "<step> <dir>" in fail.
type stackCommands struct {
	events *[]string
	fail   map[string]error
}

func (c stackCommands) Exec(_ context.Context, dir, step, _ string, out io.Writer) error {
	verb, noise := "up", "pulling images\n"
	if step == stack.Down {
		verb, noise = "down", "network busy\n"
	}
	*c.events = append(*c.events, verb+" "+dir)
	if _, err := io.WriteString(out, noise); err != nil {
		return err
	}
	return c.fail[verb+" "+dir]
}

// repoFileIn writes a repo file declaring a stack into the directory dir.
func repoFileIn(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte(declaredStack), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stackBench is a journal whose every task worktree is one real directory
// holding a repo file that declares a stack, as a recycled worktree is.
type stackBench struct {
	*journal
	dir string
}

func (b stackBench) Open(ctx context.Context, task int) (string, error) {
	if _, err := b.journal.Open(ctx, task); err != nil {
		return "", err
	}
	return b.dir, nil
}

// stackedJournalLoop is a journalLoop whose worktree is dir and whose stack is
// a real one over commands that log into the journal.
func stackedJournalLoop(t *testing.T, j *journal, fail map[string]error) (loop.Loop, string) {
	t.Helper()
	dir := repoFileIn(t, t.TempDir())
	l := journalLoop(t, j)
	l.Bench = stackBench{journal: j, dir: dir}
	l.Stack = &stack.Stack{Command: stackCommands{events: &j.events, fail: fail}}
	return l, dir
}

// reviewTree is a review bench whose review worktree is the real directory dir.
type reviewTree struct {
	reviewBench
	dir string
}

func (r reviewTree) OpenReview(ctx context.Context, issue int) (string, error) {
	if _, err := r.reviewBench.OpenReview(ctx, issue); err != nil {
		return "", err
	}
	return r.dir, nil
}

func TestRunBringsTheStackUpBeforeEveryAttemptAndDownAtTheEnd(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}}
	l, d := stackedJournalLoop(t, j, nil)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{
		"open 43", "up " + d, "hand 43 in " + d,
		"open 43", "up " + d, "hand 43 in " + d,
		"open 44", "up " + d, "hand 44 in " + d, "land 44", "push smith/42-spec",
		"down " + d,
	}
	if !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunTakesTheStackDownBeforeTheFinishedWorktreeIsRemoved(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, d := stackedJournalLoop(t, j, nil)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"open 43", "up " + d, "hand 43 in " + d, "land 43", "push smith/42-spec", "down " + d, "finish"}
	if !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunKeepsTheStackUpAcrossTasksInARecycledWorktree(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l, _ := stackedJournalLoop(t, j, nil)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if downs := countPrefix(j.events, "down "); downs != 1 {
		t.Errorf("Run() events = %q, want down once, at the end", j.events)
	}
}

func TestRunTakesTheStackDownHoweverItEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tasks []tracker.Task
		setup func(l *loop.Loop, j *journal, cancel context.CancelFunc)
	}{
		{"stopped", []tracker.Task{agentTask(43), agentTask(44)}, func(l *loop.Loop, j *journal, cancel context.CancelFunc) {
			l.Launcher = failing{board: j.board, cancel: cancel}
		}},
		{"cap reached", []tracker.Task{agentTask(43), agentTask(44)}, func(l *loop.Loop, _ *journal, _ context.CancelFunc) { l.Limits.MaxIterations = 1 }},
		{"failed", []tracker.Task{agentTask(43), agentTask(44)}, func(_ *loop.Loop, j *journal, _ context.CancelFunc) { j.pushErr = errors.New("rejected") }},
		{"waiting on human", []tracker.Task{agentTask(43), humanTask(44)}, nil},
		{"attempts exhausted", []tracker.Task{agentTask(43), agentTask(44)}, func(_ *loop.Loop, j *journal, _ context.CancelFunc) { j.board.leaveOpen = []int{44} }},
		{"complete", []tracker.Task{agentTask(43)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			j := &journal{board: &board{tasks: tt.tasks}}
			l, _ := stackedJournalLoop(t, j, nil)
			if tt.setup != nil {
				tt.setup(&l, j, cancel)
			}

			_, err := l.Run(ctx, 42)

			if downs := countPrefix(j.events, "down "); downs != 1 {
				t.Errorf("Run() = %v, events %q; want down once", err, j.events)
			}
		})
	}
}

func TestRunWhoseUpFailsStartsNoAgentTakesTheStackDownAndNamesTheTaskAndWorktree(t *testing.T) {
	t.Parallel()
	errUp := errors.New("the stack up step exited with status 1: make up")
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l, d := stackedJournalLoop(t, j, nil)
	l.Stack = &stack.Stack{Command: stackCommands{events: &j.events, fail: map[string]error{"up " + d: errUp}}}
	l.Progress = &progress
	r := &recorder{}
	l.Recorder = r

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"open 43", "up " + d, "down " + d}
	report := fmt.Sprintf("spec #42 failed: bring up the stack of #43 in %[1]s: bring up the stack in %[1]s: the stack up step exited with status 1: make up (system packages and groups come from the blueprint, not the repo file)\nwork branch: smith/42-spec\n", d)
	if !errors.Is(err, errUp) || !slices.Equal(j.events, want) || len(j.board.launched) != 0 ||
		len(r.reports) != 1 || r.reports[0] != report || !strings.Contains(progress.String(), "pulling images\n") {
		t.Errorf("Run() = %+v, %v; events %q, reports %q, progress %q; want a failed run with up's output and events %q", outcome, err, j.events, r.reports, progress.String(), want)
	}
}

func TestRunSaysOnOneLineThatTheStackIsComingUpAndKeepsASuccessfulUpsOutputBack(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l, _ := stackedJournalLoop(t, j, nil)
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(progress.String(), "smith: bringing up the stack of #43\n") || strings.Contains(progress.String(), "pulling images") {
		t.Errorf("progress = %q, want the one line and no output of up", progress.String())
	}
}

func TestAFailedDownIsAWarningThatKeepsTheOutcome(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l, d := stackedJournalLoop(t, j, nil)
	l.Stack = &stack.Stack{Command: stackCommands{events: &j.events, fail: map[string]error{"down " + d: errors.New("exit status 2")}}}
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := "smith: warning: the stack did not stop: take down the stack in " + d + ": exit status 2\nnetwork busy\n"
	if err != nil || !outcome.Complete() || !strings.Contains(progress.String(), want) {
		t.Errorf("Run() = %+v, %v, progress %q; want the run complete and progress holding %q", outcome, err, progress.String(), want)
	}
}

func TestRunWithNoStackRunsNoStackCommand(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}

	if _, err := journalLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if countPrefix(j.events, "up ") != 0 || countPrefix(j.events, "down ") != 0 {
		t.Errorf("events = %q, want no stack command", j.events)
	}
}

func TestRunOfARepoThatDeclaresNoStackRunsNoStackCommand(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, d := stackedJournalLoop(t, j, nil)
	if err := os.WriteFile(filepath.Join(d, ".smith", "repo.yaml"), []byte("push: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if countPrefix(j.events, "up ") != 0 || countPrefix(j.events, "down ") != 0 {
		t.Errorf("events = %q, want no stack command", j.events)
	}
}

func TestRunInteractiveBringsTheStackUpBeforeTheAgentAndDownAfter(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l, d := stackedJournalLoop(t, j, nil)

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	want := []string{"open 43", "up " + d, "hand 43 in " + d, "land 43", "push smith/42-spec", "down " + d}
	if !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", j.events, want)
	}
}

func TestRunBringsTheStackUpBeforeTheReview(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, noFindings)
	dir := repoFileIn(t, t.TempDir())
	l.Bench = reviewTree{reviewBench: l.Bench.(reviewBench), dir: dir}
	l.Stack = &stack.Stack{Command: stackCommands{events: &b.events}}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	i, j := slices.Index(b.events, "up "+dir), slices.Index(b.events, "launch review")
	if i < 0 || j < i || countPrefix(b.events, "down ") != 1 {
		t.Errorf("events = %q, want up before the review launch and one down", b.events)
	}
}

// countPrefix counts the events that start with prefix.
func countPrefix(events []string, prefix string) int {
	n := 0
	for _, e := range events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func TestAFailedDownIsSaidInTheStopReportOfACompleteRun(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, d := stackedJournalLoop(t, j, nil)
	l.Stack = &stack.Stack{Command: stackCommands{events: &j.events, fail: map[string]error{"down " + d: errors.New("the stack down step exited with status 2: make down")}}}
	r := &recorder{}
	l.Recorder = r

	_, err := l.Run(t.Context(), 42)

	want := "  the stack did not stop: take down the stack in " + d + ": the stack down step exited with status 2: make down\n"
	if err != nil || len(r.reports) != 1 || !strings.Contains(r.reports[0], want) || r.endings[0] != loop.Complete {
		t.Errorf("Run() = %v, endings %v, reports %q; want a complete run whose report holds %q", err, r.endings, r.reports, want)
	}
}

func TestASuccessfulDownLeavesTheStopReportAlone(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	r := &recorder{}
	l.Recorder = r

	_, err := l.Run(t.Context(), 42)

	if err != nil || len(r.reports) != 1 || strings.Contains(r.reports[0], "did not stop") {
		t.Errorf("Run() = %v, reports %q; want no mention of the stack", err, r.reports)
	}
}

// leftByADeadRun makes a stack with a state home that holds the record of a
// dead run's stack in a real directory, and returns the stack and that directory.
func leftByADeadRun(t *testing.T, events *[]string, fail map[string]error) (*stack.Stack, string) {
	t.Helper()
	state, dead := t.TempDir(), repoFileIn(t, t.TempDir())
	data, err := json.Marshal(stack.Record{Dir: dead, Branch: "smith/9-z", Up: "make up", Down: "make down"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "stacks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "stacks", "repo.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return &stack.Stack{Command: stackCommands{events: events, fail: fail}, State: state, Key: "repo", Branch: "smith/42-spec"}, dead
}

func TestRunRecoversAStackLeftBehindBeforeAnythingElseAndSaysSo(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Progress = &progress
	s, dead := leftByADeadRun(t, &j.events, nil)
	l.Recoverer = s

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if j.events[0] != "down "+dead {
		t.Errorf("events = %q, want the dead run's down first", j.events)
	}
	want := "smith: recovering the stack of work branch smith/9-z left in " + dead + "\n"
	if !strings.Contains(progress.String(), want) || strings.Contains(progress.String(), "network busy") {
		t.Errorf("progress = %q, want %q and no output of a good recovery", progress.String(), want)
	}
}

func TestRunWhoseRecoveryFailsWarnsAndGoesOn(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l, d := stackedJournalLoop(t, j, nil)
	l.Progress = &progress
	s, dead := leftByADeadRun(t, &j.events, nil)
	s.Command = stackCommands{events: &j.events, fail: map[string]error{"down " + dead: errors.New("exit status 3")}}
	l.Recoverer = s

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || !outcome.Complete() {
		t.Fatalf("Run() = %+v, %v, want the run complete", outcome, err)
	}
	if want := "smith: warning: the stack left behind was not taken down: "; !strings.Contains(progress.String(), want) || !strings.Contains(progress.String(), "exit status 3\nnetwork busy\n") {
		t.Errorf("progress = %q, want %q and the failure", progress.String(), want)
	}
	if !slices.Contains(j.events, "up "+d) {
		t.Errorf("events = %q, want the run's own up", j.events)
	}
}

func TestRunInteractiveRecoversAStackLeftBehindFirst(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l := journalLoop(t, j)
	s, dead := leftByADeadRun(t, &j.events, nil)
	l.Recoverer = s

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if j.events[0] != "down "+dead {
		t.Errorf("events = %q, want the dead run's down first", j.events)
	}
}
