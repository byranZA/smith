package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/tracker"
)

// stacking fakes a stack, logging "up <dir>" and "down <dir>" into events as
// the real one runs them: down only when a stack is up, and once.
type stacking struct {
	events  *[]string
	upErr   error
	downErr error
	dir     string
	up      bool
}

func (s *stacking) Ensure(_ context.Context, dir string, out io.Writer) error {
	*s.events = append(*s.events, "up "+dir)
	s.dir, s.up = dir, true
	_, _ = io.WriteString(out, "pulling images\n")
	return s.upErr
}

func (s *stacking) Release(_ context.Context, out io.Writer) error {
	if !s.up {
		return nil
	}
	s.up = false
	*s.events = append(*s.events, "down "+s.dir)
	_, _ = io.WriteString(out, "network busy\n")
	return s.downErr
}

func TestRunBringsTheStackUpBeforeEveryAttemptAndDownAtTheEnd(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}}
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{
		"open 43", "up /bench/43", "hand 43 in /bench/43",
		"open 43", "up /bench/43", "hand 43 in /bench/43",
		"open 44", "up /bench/44", "hand 44 in /bench/44", "land 44", "push smith/42-spec",
		"down /bench/44",
	}
	if !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunTakesTheStackDownBeforeTheFinishedWorktreeIsRemoved(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"open 43", "up /bench/43", "hand 43 in /bench/43", "land 43", "push smith/42-spec", "down /bench/43", "finish"}
	if !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunKeepsTheStackUpAcrossTasksInARecycledWorktree(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events}

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
			l := journalLoop(t, j)
			l.Stack = &stacking{events: &j.events}
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
	l := journalLoop(t, j)
	l.Stack, l.Progress = &stacking{events: &j.events, upErr: errUp}, &progress
	r := &recorder{}
	l.Recorder = r

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"open 43", "up /bench/43", "down /bench/43"}
	report := "spec #42 failed: bring up the stack of #43 in /bench/43: the stack up step exited with status 1: make up (system packages and groups come from the blueprint, not the repo file)\nwork branch: smith/42-spec\n"
	if !errors.Is(err, errUp) || !slices.Equal(j.events, want) || len(j.board.launched) != 0 ||
		len(r.reports) != 1 || r.reports[0] != report || !strings.Contains(progress.String(), "pulling images\n") {
		t.Errorf("Run() = %+v, %v; events %q, reports %q, progress %q; want a failed run with up's output and events %q", outcome, err, j.events, r.reports, progress.String(), want)
	}
}

func TestRunSaysOnOneLineThatTheStackIsComingUpAndKeepsASuccessfulUpsOutputBack(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Stack, l.Progress = &stacking{events: &j.events}, &progress

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
	l := journalLoop(t, j)
	l.Stack, l.Progress = &stacking{events: &j.events, downErr: errors.New("exit status 2")}, &progress

	outcome, err := l.Run(t.Context(), 42)

	want := "smith: warning: the stack did not stop: exit status 2\nnetwork busy\n"
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

func TestRunInteractiveBringsTheStackUpBeforeTheAgentAndDownAfter(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events}

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	want := []string{"open 43", "up /bench/43", "hand 43 in /bench/43", "land 43", "push smith/42-spec", "down /bench/43"}
	if !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", j.events, want)
	}
}

func TestRunBringsTheStackUpBeforeTheReview(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, _ := reviewingLoop(t, b, noFindings)
	l.Stack = &stacking{events: &b.events}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	i, j := slices.Index(b.events, "up /bench/review"), slices.Index(b.events, "launch review")
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
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events, downErr: errors.New("take down the stack in /bench/43: the stack down step exited with status 2: make down")}
	r := &recorder{}
	l.Recorder = r

	_, err := l.Run(t.Context(), 42)

	want := "  the stack did not stop: take down the stack in /bench/43: the stack down step exited with status 2: make down\n"
	if err != nil || len(r.reports) != 1 || !strings.Contains(r.reports[0], want) || r.endings[0] != loop.Complete {
		t.Errorf("Run() = %v, endings %v, reports %q; want a complete run whose report holds %q", err, r.endings, r.reports, want)
	}
}

func TestASuccessfulDownLeavesTheStopReportAlone(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l := journalLoop(t, j)
	l.Stack = &stacking{events: &j.events}
	r := &recorder{}
	l.Recorder = r

	_, err := l.Run(t.Context(), 42)

	if err != nil || len(r.reports) != 1 || strings.Contains(r.reports[0], "did not stop") {
		t.Errorf("Run() = %v, reports %q; want no mention of the stack", err, r.reports)
	}
}

// recovering fakes the recovery of a stack a dead run left, logging
// "recover" into events.
type recovering struct {
	events *[]string
	rec    *stack.Record
	err    error
}

func (r *recovering) Recover(_ context.Context, out io.Writer) (*stack.Record, error) {
	*r.events = append(*r.events, "recover")
	_, _ = io.WriteString(out, "no such container\n")
	return r.rec, r.err
}

func TestRunRecoversAStackLeftBehindBeforeAnythingElseAndSaysSo(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Progress = &progress
	l.Recoverer = &recovering{events: &j.events, rec: &stack.Record{Dir: "/bench/9", Branch: "smith/9-z"}}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if j.events[0] != "recover" {
		t.Errorf("events = %q, want recover first", j.events)
	}
	want := "smith: recovering the stack of work branch smith/9-z left in /bench/9\n"
	if !strings.Contains(progress.String(), want) || strings.Contains(progress.String(), "no such container") {
		t.Errorf("progress = %q, want %q and no output of a good recovery", progress.String(), want)
	}
}

func TestRunWhoseRecoveryFailsWarnsAndGoesOn(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Progress = &progress
	l.Stack = &stacking{events: &j.events}
	l.Recoverer = &recovering{events: &j.events, rec: &stack.Record{Dir: "/bench/9", Branch: "b"}, err: errors.New("exit status 3")}

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || !outcome.Complete() {
		t.Fatalf("Run() = %+v, %v, want the run complete", outcome, err)
	}
	if want := "smith: warning: the stack left behind was not taken down: exit status 3\nno such container\n"; !strings.Contains(progress.String(), want) {
		t.Errorf("progress = %q, want %q", progress.String(), want)
	}
	if !slices.Contains(j.events, "up /bench/43") {
		t.Errorf("events = %q, want the run's own up", j.events)
	}
}

func TestRunInteractiveRecoversAStackLeftBehindFirst(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l := journalLoop(t, j)
	l.Recoverer = &recovering{events: &j.events, rec: &stack.Record{Dir: "/bench/9", Branch: "b"}}

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if j.events[0] != "recover" {
		t.Errorf("events = %q, want recover first", j.events)
	}
}
