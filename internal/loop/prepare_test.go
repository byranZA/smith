package loop_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// preparing fakes worktree preparation, logging each worktree it prepares among board's events and failing each with fail.
type preparing struct {
	board *board
	fail  error
}

func (p preparing) Prepare(_ context.Context, dir string, _ io.Writer) error {
	p.board.events = append(p.board.events, "prepare "+dir)
	return p.fail
}

// errPrepare is the failure a fake preparation answers with.
var errPrepare = errors.New("the prepare step exited with status 2: npm ci")

func TestRunPreparesTheTaskWorktreeBeforeEveryAgentAttempt(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := loopOn(t, b, b)
	l.Preparer = preparing{board: b}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"prepare /bench/43", "start 43", "launch 43", "stop 43", "prepare /bench/43", "start 43", "launch 43", "fail 43"}
	if !slices.Equal(b.events, want) {
		t.Errorf("Run() events = %q, want %q", b.events, want)
	}
}

func TestRunInteractivePreparesTheTaskWorktreeBeforeTheAgent(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Preparer = preparing{board: b}

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"prepare /bench/43", "start 43", "launch 43", "stop 43"}; !slices.Equal(b.events, want) {
		t.Errorf("RunInteractive() events = %q, want %q", b.events, want)
	}
}

func TestRunStopsAsFailedWithoutAnAgentWhenPreparationFails(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	r := &recorder{}
	l := loopOn(t, b, b)
	l.Preparer, l.Recorder = preparing{board: b, fail: errPrepare}, r

	_, err := l.Run(t.Context(), 42)

	if !errors.Is(err, errPrepare) || len(b.launched) != 0 || !reflect.DeepEqual(r.endings, []loop.Ending{loop.Failed}) {
		t.Errorf("Run() = %v after %d agent runs, recorded %q; want the preparation error, no agent run and %q", err, len(b.launched), r.endings, loop.Failed)
	}
}

func TestRunWhosePreparationFailsMarksNoTaskAndCommentsOnlyOnTheExecution(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Preparer = preparing{board: b, fail: errPrepare}

	if _, err := l.Run(t.Context(), 42); err == nil {
		t.Fatal("Run() error = nil, want the preparation error")
	}

	if want := []string{"prepare /bench/43"}; !slices.Equal(b.events, want) || len(b.comments) != 1 || b.comments[0].number != 42 {
		t.Errorf("Run() events = %q, comments %+v; want %q and only the stop report on #42", b.events, b.comments, want)
	}
}

func TestRunWhosePreparationFailsRecordsNoAttemptAtTheTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Preparer = preparing{board: b, fail: errPrepare}

	outcome, err := l.Run(t.Context(), 42)

	if err == nil || !reflect.DeepEqual(outcome, loop.Outcome{}) {
		t.Errorf("Run() = %+v, %v; want no outcome, so no attempt or skip, and the preparation error", outcome, err)
	}
}

func TestRunWhosePreparationFailsReportsPreparationTheTaskAndItsWorktree(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	r := &recorder{}
	l := loopOn(t, b, b)
	l.Preparer, l.Recorder = preparing{board: b, fail: errPrepare}, r

	if _, err := l.Run(t.Context(), 42); err == nil {
		t.Fatal("Run() error = nil, want the preparation error")
	}

	want := []string{"spec #42 failed: prepare the worktree of #43 in /bench/43: the prepare step exited with status 2: npm ci\nwork branch: smith/42-spec\n"}
	if !reflect.DeepEqual(r.reports, want) {
		t.Errorf("Run() recorded %q, want %q", r.reports, want)
	}
}
