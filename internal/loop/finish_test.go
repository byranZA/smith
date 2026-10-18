package loop_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func TestACompleteRunFinishesTheBenchAfterItsLastPush(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}

	if _, err := journalLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{"open 43", "hand 43 in /bench/43", "land 43", "push smith/42-spec", "finish"}; !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestAnUnfinishedRunKeepsTheRecycledWorktree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		board board
		setup func(l *loop.Loop, j *journal, cancel context.CancelFunc)
	}{
		{"stopped", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}, func(l *loop.Loop, j *journal, cancel context.CancelFunc) {
			l.Launcher = failing{board: j.board, cancel: cancel}
		}},
		{"cap reached", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}, func(l *loop.Loop, _ *journal, _ context.CancelFunc) {
			l.Limits.MaxIterations = 1
		}},
		{"failed", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}, func(_ *loop.Loop, j *journal, _ context.CancelFunc) {
			j.pushErr = errors.New("rejected")
		}},
		{"waiting on human", board{tasks: []tracker.Task{agentTask(43), humanTask(44)}}, nil},
		{"attempts exhausted", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{44}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			j := &journal{board: &tt.board}
			l := journalLoop(t, j)
			if tt.setup != nil {
				tt.setup(&l, j, cancel)
			}

			_, err := l.Run(ctx, 42)

			if slices.Contains(j.events, "finish") {
				t.Errorf("Run() = %v, events %q; want no finish", err, j.events)
			}
		})
	}
}

func TestAFailedFinishIsAWarningOnACompleteRun(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}, finishErr: errors.New("worktree is dirty")}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := "smith: warning: could not remove the recycled task worktree of smith/42-spec: worktree is dirty\n"
	if err != nil || !outcome.Complete() || !strings.HasSuffix(progress.String(), want) {
		t.Errorf("Run() = %+v, %v, progress %q; want the spec complete and progress ending %q", outcome, err, progress.String(), want)
	}
}

func TestAnInteractiveRunClosingTheLastTaskFinishesTheBenchAfterItsPush(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{closed(agentTask(44)), agentTask(43)}}}

	if _, err := journalLoop(t, j).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"open 43", "hand 43 in /bench/43", "land 43", "push smith/42-spec", "finish"}; !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", j.events, want)
	}
}

func TestAnInteractiveRunOnACompleteExecutionFinishesTheBench(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{closed(agentTask(43))}}}

	if _, err := journalLoop(t, j).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"finish"}; !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", j.events, want)
	}
}

func TestAnUnfinishedInteractiveRunKeepsTheRecycledWorktree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		board board
	}{
		{"tasks remain", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}},
		{"task left open", board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}},
		{"waiting on human", board{tasks: []tracker.Task{closed(agentTask(43)), humanTask(44)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			j := &journal{board: &tt.board}

			_, err := journalLoop(t, j).RunInteractive(t.Context(), 42)

			if err != nil || slices.Contains(j.events, "finish") {
				t.Errorf("RunInteractive() = %v, events %q; want no error and no finish", err, j.events)
			}
		})
	}
}

func TestAFailedFinishIsAWarningOnACompleteInteractiveRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tasks []tracker.Task
	}{
		{"last task closed", []tracker.Task{agentTask(43)}},
		{"already complete", []tracker.Task{closed(agentTask(43))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			j := &journal{board: &board{tasks: tt.tasks}, finishErr: errors.New("worktree is dirty")}
			var progress strings.Builder
			l := journalLoop(t, j)
			l.Progress = &progress

			outcome, err := l.RunInteractive(t.Context(), 42)

			want := "smith: warning: could not remove the recycled task worktree of smith/42-spec: worktree is dirty\n"
			if err != nil || !outcome.Succeeded() || !strings.HasSuffix(progress.String(), want) {
				t.Errorf("RunInteractive() = %+v, %v, progress %q; want success and progress ending %q", outcome, err, progress.String(), want)
			}
		})
	}
}
