package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// board is a fake tracker holding spec #42, and a fake launcher whose agent
// closes each task it is handed unless that task is listed in leaveOpen.
type board struct {
	tasks     []tracker.Task
	leaveOpen []int
	reads     int
	launched  []agent.Command
}

func (b *board) Spec(_ context.Context, number int) (tracker.Spec, error) {
	b.reads++
	return tracker.Spec{Number: number, Title: "spec", Tasks: slices.Clone(b.tasks)}, nil
}

func (b *board) Launch(_ context.Context, cmd agent.Command) error {
	b.launched = append(b.launched, cmd)
	n, err := strconv.Atoi(cmd.Args[len(cmd.Args)-1])
	if err != nil {
		return err
	}
	if slices.Contains(b.leaveOpen, n) {
		return nil
	}
	for i := range b.tasks {
		if b.tasks[i].Number == n {
			b.tasks[i].Open = false
		}
	}
	return nil
}

func (b *board) handed() []string {
	var prompts []string
	for _, cmd := range b.launched {
		prompts = append(prompts, cmd.Args[len(cmd.Args)-1])
	}
	return prompts
}

func run(t *testing.T, b *board) loop.Outcome {
	t.Helper()
	outcome, err := loopOn(t, b, b).Run(context.Background(), 42)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return outcome
}

func TestRunHandsEachTaskToItsOwnAgentRunInOrder(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	outcome := run(t, b)

	if got, want := b.handed(), []string{"43", "44"}; !slices.Equal(got, want) || !outcome.Complete() {
		t.Errorf("handed %v, complete %v; want %v and the spec complete", got, outcome.Complete(), want)
	}
}

func TestRunStopsWithoutAnAgentWhenOnlyBlockedOrHumanTasksRemain(t *testing.T) {
	b := &board{tasks: []tracker.Task{humanTask(43), agentTask(44, tracker.Blocker{Number: 43, Open: true})}}

	outcome := run(t, b)

	if len(b.launched) != 0 || outcome.Complete() || !slices.Equal(numbers(outcome.Remaining.Human), []int{43}) || !slices.Equal(numbers(outcome.Remaining.Blocked), []int{44}) {
		t.Errorf("launched %d, outcome %+v; want no agent run, #43 waiting on a human and #44 blocked", len(b.launched), outcome)
	}
}

func TestRunOnACompleteSpecRunsNoAgent(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43)), closed(agentTask(44))}}

	outcome := run(t, b)

	if len(b.launched) != 0 || !outcome.Complete() {
		t.Errorf("launched %d, complete %v; want no agent run and the spec complete", len(b.launched), outcome.Complete())
	}
}

func TestRunStopsWhenTheAgentLeavesItsTaskOpen(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}

	outcome := run(t, b)

	if got := b.handed(); !slices.Equal(got, []string{"43"}) || !slices.Equal(numbers(outcome.LeftOpen), []int{43}) || outcome.Complete() {
		t.Errorf("handed %v, outcome %+v; want #43 handed once and named as left open", got, outcome)
	}
}

func TestRunPicksUpATaskFiledMidRun(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	filing := &filer{board: b, file: agentTask(50)}

	if _, err := loopOn(t, b, filing).Run(context.Background(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := b.handed(), []string{"43", "50"}; !slices.Equal(got, want) {
		t.Errorf("handed %v, want %v", got, want)
	}
}

// filer is a launcher whose agent also files a new task against the spec on its first run.
type filer struct {
	board *board
	file  tracker.Task
	filed bool
}

func (f *filer) Launch(ctx context.Context, cmd agent.Command) error {
	if !f.filed {
		f.board.tasks = append(f.board.tasks, f.file)
		f.filed = true
	}
	return f.board.Launch(ctx, cmd)
}

func TestRunStartsNoFurtherTaskOnceInterrupted(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := loopOn(t, b, failing{board: b, cancel: cancel}).Run(ctx, 42)

	if !errors.Is(err, context.Canceled) || !slices.Equal(b.handed(), []string{"43"}) {
		t.Errorf("Run() error = %v, handed %v; want context.Canceled after #43 alone", err, b.handed())
	}
}

func TestRunGoesByTheTrackerWhenAnAgentExitsWithAFailure(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	outcome, err := loopOn(t, b, failing{board: b}).Run(context.Background(), 42)

	if err != nil || !outcome.Complete() || !slices.Equal(b.handed(), []string{"43", "44"}) {
		t.Errorf("Run() = %+v, %v, handed %v; want both tasks worked and the spec complete", outcome, err, b.handed())
	}
}

func loopOn(t *testing.T, b *board, launcher loop.Launcher) loop.Loop {
	t.Helper()
	claude, err := agent.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(claude) error = %v", err)
	}
	return loop.Loop{Tracker: b, Launcher: launcher, Agent: claude, Prompt: "{{TASK_NUMBER}}", Progress: io.Discard}
}

// failing is a launcher whose agent does its work and then exits with a failure, cancelling the run first when cancel is set.
type failing struct {
	board  *board
	cancel context.CancelFunc
}

func (f failing) Launch(ctx context.Context, cmd agent.Command) error {
	if err := f.board.Launch(ctx, cmd); err != nil {
		return err
	}
	if f.cancel != nil {
		f.cancel()
	}
	return errors.New("exit status 1")
}
