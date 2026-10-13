package loop_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// journal is a launcher, bench and pusher that logs each worktree opened, agent run, landing and push, in order, over a board.
type journal struct {
	board   *board
	events  []string
	landErr error
	pushErr error
}

func (j *journal) Launch(ctx context.Context, cmd agent.Command) error {
	j.events = append(j.events, "hand "+taskOf(cmd)+" in "+cmd.Dir)
	return j.board.Launch(ctx, cmd)
}

func (j *journal) WorkBranch() string { return "smith/42-spec" }

func (j *journal) Open(_ context.Context, task int) (string, error) {
	j.events = append(j.events, "open "+strconv.Itoa(task))
	return worktreeOf(task), nil
}

func (j *journal) Land(_ context.Context, task int) error {
	j.events = append(j.events, "land "+strconv.Itoa(task))
	return j.landErr
}

func (j *journal) Push(_ context.Context, branch string) error {
	j.events = append(j.events, "push "+branch)
	return j.pushErr
}

func journalLoop(t *testing.T, j *journal) loop.Loop {
	t.Helper()
	l := loopOn(t, j.board, j)
	l.Bench = j
	l.Pusher = j
	return l
}

func TestRunWorksEachTaskInItsWorktreeThenLandsAndPushesItBeforeHandingOutTheNext(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}

	if _, err := journalLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{
		"open 43", "hand 43 in /bench/43", "land 43", "push smith/42-spec",
		"open 44", "hand 44 in /bench/44", "land 44", "push smith/42-spec",
	}
	if !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunNeitherLandsNorPushesATaskLeftOpen(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}}

	if _, err := journalLoop(t, j).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{"open 43", "hand 43 in /bench/43", "open 43", "hand 43 in /bench/43"}; !slices.Equal(j.events, want) {
		t.Errorf("Run() events = %q, want %q", j.events, want)
	}
}

func TestRunWithPushingOffLandsEachTaskAndPushesNothing(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l := journalLoop(t, j)
	l.Pusher = nil

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"open 43", "hand 43 in /bench/43", "land 43", "open 44", "hand 44 in /bench/44", "land 44"}
	if err != nil || !outcome.Complete() || !slices.Equal(j.events, want) {
		t.Errorf("Run() = %+v, %v, events %q; want the spec complete and events %q", outcome, err, j.events, want)
	}
}

func TestRunStopsOnAFailedLandingNamingTheTaskWithNothingPushed(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	j := &journal{board: b, landErr: errors.New("worktree has uncommitted changes")}

	_, err := journalLoop(t, j).Run(t.Context(), 42)

	want := []string{"open 43", "hand 43 in /bench/43", "land 43"}
	if err == nil || err.Error() != "land the work on #43: worktree has uncommitted changes" || !slices.Equal(j.events, want) {
		t.Errorf("Run() error = %v, events %q; want the landing error naming #43 and events %q", err, j.events, want)
	}
}

func TestRunStopsOnAFailedPushNamingTheTaskAndTheError(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	j := &journal{board: b, pushErr: errors.New("remote rejected")}

	_, err := journalLoop(t, j).Run(t.Context(), 42)

	if err == nil || err.Error() != "push the work on #43: remote rejected" || !slices.Equal(b.handed(), []string{"43"}) || b.tasks[0].Open {
		t.Errorf("Run() error = %v, handed %v, #43 open %v; want the push error naming #43, #44 never handed and #43 left closed", err, b.handed(), b.tasks[0].Open)
	}
}

func TestRunNamesThePushedWorkBranch(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	var progress strings.Builder
	l := journalLoop(t, j)
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "smith: pushed #43 on smith/42-spec to origin\n"; !strings.HasSuffix(progress.String(), want) {
		t.Errorf("Run() progress = %q, want it to end %q", progress.String(), want)
	}
}

func TestRunReportsTheWorkBranch(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}

	outcome, err := journalLoop(t, j).Run(t.Context(), 42)

	if err != nil || outcome.WorkBranch != "smith/42-spec" {
		t.Errorf("Run() = %+v, %v; want the work branch smith/42-spec", outcome, err)
	}
}

func TestRunInteractiveWorksTheTaskInItsWorktreeThenLandsAndPushesIt(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}

	outcome, err := journalLoop(t, j).RunInteractive(t.Context(), 42)

	want := []string{"open 43", "hand 43 in /bench/43", "land 43", "push smith/42-spec"}
	if err != nil || outcome.WorkBranch != "smith/42-spec" || !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive() = %+v, %v, events %q; want the work branch smith/42-spec and events %q", outcome, err, j.events, want)
	}
}
