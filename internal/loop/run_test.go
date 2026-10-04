package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// board fakes spec #42's tracker and an agent that closes each task not in leaveOpen.
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
	n, err := strconv.Atoi(taskOf(cmd))
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
		prompts = append(prompts, taskOf(cmd))
	}
	return prompts
}

// taskOf is the task number a prompt of "{{TASK_NUMBER}}", with any note after it, hands the agent.
func taskOf(cmd agent.Command) string {
	return strings.Fields(cmd.Args[len(cmd.Args)-1])[0]
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

func TestRunRetriesATaskLeftOpenThenSkipsItAndWorksTheRest(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}

	outcome := run(t, b)

	if got := b.handed(); !slices.Equal(got, []string{"43", "43", "44"}) || !slices.Equal(numbers(outcome.Remaining.Skipped), []int{43}) || outcome.Complete() {
		t.Errorf("handed %v, outcome %+v; want #43 twice then #44, and #43 named as skipped", got, outcome)
	}
}

func TestRunStopsAtTheIterationCap(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44), agentTask(45)}, leaveOpen: []int{43, 44, 45}}
	l := loopOn(t, b, b)
	l.Limits.MaxIterations = 3

	outcome, err := l.Run(context.Background(), 42)

	if err != nil || len(b.launched) != 3 || !outcome.Capped || outcome.Complete() {
		t.Errorf("Run() = %+v, %v after %d agent runs; want 3 runs and the cap reported", outcome, err, len(b.launched))
	}
}

func TestRunThatCompletesTheSpecOnItsLastAllowedRunIsNotCapped(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	l := loopOn(t, b, b)
	l.Limits.MaxIterations = 2

	outcome, err := l.Run(context.Background(), 42)

	if err != nil || outcome.Capped || !outcome.Complete() {
		t.Errorf("Run() = %+v, %v; want the spec complete and not capped", outcome, err)
	}
}

func TestRunLeavesAFailedAttemptsWorkForTheNextAttempt(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	tree := &wip{board: b}

	if _, err := loopOn(t, b, tree).Run(context.Background(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !slices.Equal(tree.found, []string{"", "half done"}) {
		t.Errorf("working tree seen by each attempt = %q, want empty then the first attempt's changes", tree.found)
	}
}

// wip is a launcher whose agent leaves uncommitted changes in a working tree on its first attempt and finishes on its second.
type wip struct {
	board *board
	tree  string
	found []string
}

func (w *wip) Launch(ctx context.Context, cmd agent.Command) error {
	w.found = append(w.found, w.tree)
	if w.tree == "" {
		w.tree = "half done"
		return nil
	}
	return w.board.Launch(ctx, cmd)
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
	return loop.Loop{Tracker: b, Launcher: launcher, Agent: claude, Prompt: "{{TASK_NUMBER}}", Progress: io.Discard, Limits: loop.DefaultLimits()}
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

func TestRunAsksEveryAgentRunForTheModelAndEffort(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Options = agent.Options{Model: "opus", Effort: agent.High}

	if _, err := l.Run(context.Background(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"--permission-mode", "auto", "--model", "opus", "--effort", "high", "--print", "43"}
	if len(b.launched) != 1 || !slices.Equal(b.launched[0].Args, want) {
		t.Errorf("launched %v, want one run with args %q", b.launched, want)
	}
}

func TestRunInteractiveAttachesTheAgentForExactlyTheNextTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	outcome, err := loopOn(t, b, b).RunInteractive(context.Background(), 42)

	if err != nil || !slices.Equal(b.handed(), []string{"43"}) || !b.launched[0].Attached || outcome.Task.Number != 43 || !outcome.Closed() {
		t.Errorf("RunInteractive() = %+v, %v, launched %+v; want #43 alone handed to an attached agent and closed", outcome, err, b.launched)
	}
}

func TestRunInteractiveHandsTheAgentThePromptWithTheInteractiveNote(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}

	if _, err := loopOn(t, b, b).RunInteractive(context.Background(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := loop.Prompt("{{TASK_NUMBER}}").Interactive().Render(agentTask(43)); len(b.launched) != 1 || b.launched[0].Args[len(b.launched[0].Args)-1] != want {
		t.Errorf("launched %+v, want one run on the prompt with the interactive note", b.launched)
	}
}

func TestRunInteractiveSucceedsOnlyWhenTheTaskWasClosed(t *testing.T) {
	tests := []struct {
		name      string
		leaveOpen []int
		want      bool
	}{
		{"closed", nil, true},
		{"left open", []int{43}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: tt.leaveOpen}

			outcome, err := loopOn(t, b, b).RunInteractive(context.Background(), 42)

			if err != nil || outcome.Succeeded() != tt.want {
				t.Errorf("RunInteractive() = %+v, %v; want succeeded %v", outcome, err, tt.want)
			}
		})
	}
}

func TestRunInteractiveWithNothingAvailableRunsNoAgent(t *testing.T) {
	tests := []struct {
		name  string
		tasks []tracker.Task
		want  bool
	}{
		{"spec complete", []tracker.Task{closed(agentTask(43))}, true},
		{"only a human task", []tracker.Task{humanTask(43)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &board{tasks: tt.tasks}

			outcome, err := loopOn(t, b, b).RunInteractive(context.Background(), 42)

			if err != nil || len(b.launched) != 0 || outcome.Handed || outcome.Succeeded() != tt.want {
				t.Errorf("RunInteractive() = %+v, %v after %d agent runs; want no agent run and succeeded %v", outcome, err, len(b.launched), tt.want)
			}
		})
	}
}
