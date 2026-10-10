package loop_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// journal is a launcher and pusher that logs each agent run and push, in order, over a board.
type journal struct {
	board  *board
	events []string
	pushed loop.Pushed
	err    error
}

func (j *journal) Launch(ctx context.Context, cmd agent.Command) error {
	j.events = append(j.events, "hand "+taskOf(cmd))
	return j.board.Launch(ctx, cmd)
}

func (j *journal) Push(context.Context) (loop.Pushed, error) {
	j.events = append(j.events, "push")
	return j.pushed, j.err
}

func pushingLoop(t *testing.T, j *journal) loop.Loop {
	t.Helper()
	l := loopOn(t, j.board, j)
	l.Pusher = j
	return l
}

func TestRunPushesEachClosedTaskBeforeHandingOutTheNext(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}

	if _, err := pushingLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{"hand 43", "push", "hand 44", "push"}; !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunPushesNothingForATaskLeftOpen(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}}

	if _, err := pushingLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{"hand 43", "hand 43"}; !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunStopsOnAFailedPushNamingTheTaskAndTheError(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	j := &journal{board: b, err: errors.New("remote rejected")}

	_, err := pushingLoop(t, j).Run(t.Context(), 42)

	if err == nil || err.Error() != "push the work on #43: remote rejected" || !slices.Equal(b.handed(), []string{"43"}) || b.tasks[0].Open {
		t.Errorf("Run() error = %v, handed %v, #43 open %v; want the push error naming #43, #44 never handed and #43 left closed", err, b.handed(), b.tasks[0].Open)
	}
}

func TestRunSaysSoWhenItSkipsThePushOnTheDefaultBranch(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}, pushed: loop.Pushed{Branch: "main", Skipped: true}}
	var progress strings.Builder
	l := pushingLoop(t, j)
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: not pushing #43: main is origin's default branch\n"; !strings.HasSuffix(progress.String(), want) {
		t.Errorf("Run() progress = %q, want it to end %q", progress.String(), want)
	}
}

func TestRunNamesThePushedBranch(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}, pushed: loop.Pushed{Branch: "feat/42"}}
	var progress strings.Builder
	l := pushingLoop(t, j)
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: pushed #43 on feat/42 to origin\n"; !strings.HasSuffix(progress.String(), want) {
		t.Errorf("Run() progress = %q, want it to end %q", progress.String(), want)
	}
}

func TestRunInteractivePushesTheTaskItClosed(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}

	if _, err := pushingLoop(t, j).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"hand 43", "push"}; !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", j.events, want)
	}
}
