package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// board fakes spec #42's tracker and an agent that closes each task not in leaveOpen.
// A Fail that succeeds hands the task to a human, as the tracker does.
type board struct {
	tasks     []tracker.Task
	leaveOpen []int
	reads     int
	launched  []agent.Command
	// events logs each label write and agent launch, in order.
	events []string
	// refuse, when set, is what every label write fails with.
	refuse error
	// deadlines holds how long each logged label write had left to run, zero for no limit.
	deadlines []time.Duration
}

func (b *board) Start(ctx context.Context, number int) error {
	return b.write(ctx, "start", number)
}

func (b *board) Stop(ctx context.Context, number int) error {
	return b.write(ctx, "stop", number)
}

func (b *board) Fail(ctx context.Context, number int) error {
	if err := b.write(ctx, "fail", number); err != nil {
		return err
	}
	for i := range b.tasks {
		if b.tasks[i].Number == number {
			b.tasks[i].For = tracker.Human
		}
	}
	return nil
}

// write logs a label write of verb on task number, which like gh never reaches the tracker once ctx is done.
func (b *board) write(ctx context.Context, verb string, number int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.events = append(b.events, verb+" "+strconv.Itoa(number))
	b.deadlines = append(b.deadlines, deadlineOf(ctx))
	return b.refuse
}

// deadlineOf is how long ctx has left before its deadline, or zero when it has none.
func deadlineOf(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	return time.Until(deadline)
}

func (b *board) Execution(_ context.Context, number int) (tracker.Execution, error) {
	b.reads++
	return tracker.Execution{Number: number, Title: "spec", Tasks: slices.Clone(b.tasks)}, nil
}

func (b *board) Task(_ context.Context, number int) (tracker.Task, error) {
	for _, task := range b.tasks {
		if task.Number == number {
			return task, nil
		}
	}
	return tracker.Task{}, &tracker.NotFoundError{Number: number}
}

func (b *board) Launch(_ context.Context, cmd agent.Command) error {
	b.launched = append(b.launched, cmd)
	b.events = append(b.events, "launch "+taskOf(cmd))
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
	outcome, err := loopOn(t, b, b).Run(t.Context(), 42)
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

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || len(b.launched) != 3 || !outcome.Capped || outcome.Complete() {
		t.Errorf("Run() = %+v, %v after %d agent runs; want 3 runs and the cap reported", outcome, err, len(b.launched))
	}
}

func TestRunThatCompletesTheSpecOnItsLastAllowedRunIsNotCapped(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	l := loopOn(t, b, b)
	l.Limits.MaxIterations = 2

	outcome, err := l.Run(t.Context(), 42)

	if err != nil || outcome.Capped || !outcome.Complete() {
		t.Errorf("Run() = %+v, %v; want the spec complete and not capped", outcome, err)
	}
}

func TestRunLeavesAFailedAttemptsWorkForTheNextAttempt(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	tree := &wip{board: b}

	if _, err := loopOn(t, b, tree).Run(t.Context(), 42); err != nil {
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

	if _, err := loopOn(t, b, filing).Run(t.Context(), 42); err != nil {
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, err := loopOn(t, b, failing{board: b, cancel: cancel}).Run(ctx, 42)

	if !errors.Is(err, context.Canceled) || !slices.Equal(b.handed(), []string{"43"}) {
		t.Errorf("Run() error = %v, handed %v; want context.Canceled after #43 alone", err, b.handed())
	}
}

func TestAnInterruptedRunUnmarksTheTaskItWasWorking(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(loop.Loop, context.Context) error
	}{
		{"unattended", func(l loop.Loop, ctx context.Context) error { _, err := l.Run(ctx, 42); return err }},
		{"interactive", func(l loop.Loop, ctx context.Context) error { _, err := l.RunInteractive(ctx, 42); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			err := tt.run(loopOn(t, b, failing{board: b, cancel: cancel}), ctx)

			if want := []string{"start 43", "launch 43", "stop 43"}; !errors.Is(err, context.Canceled) || !slices.Equal(b.events, want) {
				t.Errorf("run made %q with error %v; want %q and context.Canceled", b.events, err, want)
			}
		})
	}
}

func TestAnInterruptedRunBoundsTheUnmarkingByAShortTimeout(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if _, err := loopOn(t, b, failing{board: b, cancel: cancel}).Run(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}

	if last := b.deadlines[len(b.deadlines)-1]; last <= 0 || last > 30*time.Second {
		t.Errorf("Run() unmarked #43 with %v left to run, want a limit of at most 30s", last)
	}
}

func TestAnInterruptedRunWarnsOfAFailedUnmarking(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, refuse: errors.New("HTTP 403")}
	var progress strings.Builder
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := loopOn(t, b, failing{board: b, cancel: cancel})
	l.Progress = &progress

	_, err := l.Run(ctx, 42)

	want := []string{"smith: warning: could not remove the worked mark from #43: HTTP 403"}
	if !errors.Is(err, context.Canceled) || !containsLines(progress.String(), want) {
		t.Errorf("Run() error = %v with progress\n%s\nwant context.Canceled and the lines %q", err, progress.String(), want)
	}
}

func TestRunGoesByTheTrackerWhenAnAgentExitsWithAFailure(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	outcome, err := loopOn(t, b, failing{board: b}).Run(t.Context(), 42)

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
	return loop.Loop{Tracker: b, Launcher: launcher, Bench: trees{}, Agent: claude, Prompt: "{{TASK_NUMBER}}", Progress: io.Discard, Limits: loop.DefaultLimits()}
}

// trees is a bench whose every task worktree opens and lands without fault.
type trees struct{}

func (trees) WorkBranch() string { return "smith/42-spec" }

func (trees) Open(_ context.Context, task int) (string, error) { return worktreeOf(task), nil }

func (trees) Land(context.Context, int) error { return nil }

// worktreeOf is where a fake bench puts task's worktree.
func worktreeOf(task int) string { return "/bench/" + strconv.Itoa(task) }

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

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"--permission-mode", "auto", "--model", "opus", "--effort", "high", "--print", "43"}
	if len(b.launched) != 1 || !slices.Equal(b.launched[0].Args, want) {
		t.Errorf("launched %v, want one run with args %q", b.launched, want)
	}
}

func TestRunInteractiveAttachesTheAgentForExactlyTheNextTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	outcome, err := loopOn(t, b, b).RunInteractive(t.Context(), 42)

	if err != nil || !slices.Equal(b.handed(), []string{"43"}) || !b.launched[0].Attached || outcome.Task.Number != 43 || !outcome.Closed() {
		t.Errorf("RunInteractive() = %+v, %v, launched %+v; want #43 alone handed to an attached agent and closed", outcome, err, b.launched)
	}
}

func TestRunInteractiveHandsTheAgentThePromptWithTheInteractiveNote(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}

	if _, err := loopOn(t, b, b).RunInteractive(t.Context(), 42); err != nil {
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

			outcome, err := loopOn(t, b, b).RunInteractive(t.Context(), 42)

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

			outcome, err := loopOn(t, b, b).RunInteractive(t.Context(), 42)

			if err != nil || len(b.launched) != 0 || outcome.Handed || outcome.Succeeded() != tt.want {
				t.Errorf("RunInteractive() = %+v, %v after %d agent runs; want no agent run and succeeded %v", outcome, err, len(b.launched), tt.want)
			}
		})
	}
}

func TestRunMarksATaskOnlyWhileItsAgentRuns(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}

	run(t, b)

	want := []string{"start 43", "launch 43", "stop 43", "start 44", "launch 44", "stop 44"}
	if !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q, want %q", b.events, want)
	}
}

func TestRunHandsATaskToAHumanOnlyAfterItsLastAttempt(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}

	run(t, b)

	want := []string{"start 43", "launch 43", "stop 43", "start 43", "launch 43", "fail 43", "start 44", "launch 44", "stop 44"}
	if !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q, want %q", b.events, want)
	}
}

func TestRunNeverFailsATaskClosedOnItsLastAttempt(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	tree := &wip{board: b}

	if _, err := loopOn(t, b, tree).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{"start 43", "stop 43", "start 43", "launch 43", "stop 43"}; !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q, want %q", b.events, want)
	}
}

func TestRunUnmarksTheTaskWhenTheIterationCapStopsIt(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := loopOn(t, b, b)
	l.Limits.MaxIterations = 1

	outcome, err := l.Run(t.Context(), 42)

	if want := []string{"start 43", "launch 43", "stop 43"}; err != nil || !outcome.Capped || !slices.Equal(b.events, want) {
		t.Errorf("Run() = %+v, %v after %q; want capped after %q", outcome, err, b.events, want)
	}
}

func TestRunInteractiveMarksTheTaskWhileTheAttachedAgentRuns(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}

	if _, err := loopOn(t, b, b).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"start 43", "launch 43", "stop 43"}; !slices.Equal(b.events, want) {
		t.Errorf("RunInteractive() made %q, want %q", b.events, want)
	}
}

func TestRunInteractiveNeverFailsATaskLeftOpen(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := loopOn(t, b, b)
	l.Limits.MaxAttempts = 1

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if want := []string{"start 43", "launch 43", "stop 43"}; !slices.Equal(b.events, want) {
		t.Errorf("RunInteractive() made %q, want %q", b.events, want)
	}
}

func TestRunWarnsOfAFailedHandOffAndEndsWithTheTaskSkipped(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}, refuse: errors.New("HTTP 403")}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Progress = &progress
	l.Limits.MaxAttempts = 1

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not hand #43 to a human: HTTP 403"}
	if err != nil || !slices.Equal(numbers(outcome.Remaining.Skipped), []int{43}) || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant #43 skipped and the lines %q", outcome, err, progress.String(), want)
	}
}

func TestRunWarnsOfAFailedMarkAndCarriesOn(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, refuse: errors.New("HTTP 403")}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := []string{
		"smith: warning: could not mark #43 as worked: HTTP 403",
		"smith: warning: could not remove the worked mark from #43: HTTP 403",
	}
	if err != nil || !outcome.Complete() || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant the spec complete and the lines %q", outcome, err, progress.String(), want)
	}
}

// containsLines reports whether text holds each of lines as a whole line.
func containsLines(text string, lines []string) bool {
	got := strings.Split(text, "\n")
	for _, line := range lines {
		if !slices.Contains(got, line) {
			return false
		}
	}
	return true
}

func TestRunClearsItsSpecsStaleMarksBeforeAnyAgentStarts(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{worked(closed(agentTask(43))), agentTask(44), worked(agentTask(45))}}

	run(t, b)

	want := []string{"stop 43", "stop 45", "start 44", "launch 44", "stop 44", "start 45", "launch 45", "stop 45"}
	if !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q, want %q", b.events, want)
	}
}

func TestRunWarnsOfAFailedSweepAndCarriesOn(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{worked(closed(agentTask(43)))}, refuse: errors.New("HTTP 403")}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not remove the worked mark from #43: HTTP 403"}
	if err != nil || !outcome.Complete() || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant the spec complete and the lines %q", outcome, err, progress.String(), want)
	}
}

// worked is task labelled as worked, as a run that died would leave it.
func worked(task tracker.Task) tracker.Task {
	task.Working = true
	return task
}

// unreadable is a tracker whose every spec read after the first fails, cancelling the run first when cancel is set.
type unreadable struct {
	*board
	cancel context.CancelFunc
}

// errUnreadable is what an unreadable tracker's failed spec reads fail with.
var errUnreadable = errors.New("HTTP 502")

func (u unreadable) Execution(ctx context.Context, number int) (tracker.Execution, error) {
	if u.reads == 0 {
		return u.board.Execution(ctx, number)
	}
	if u.cancel != nil {
		u.cancel()
	}
	return tracker.Execution{}, errUnreadable
}

func TestRunUnmarksTheTaskWhenTheReadAfterItsAgentFails(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Tracker = unreadable{board: b}

	_, err := l.Run(t.Context(), 42)

	if want := []string{"start 43", "launch 43", "stop 43"}; !errors.Is(err, errUnreadable) || !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q with error %v; want %q and the read's error", b.events, err, want)
	}
}

func TestRunInterruptedDuringTheReadAfterItsAgentUnmarksTheTaskWithinAShortTimeout(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := loopOn(t, b, b)
	l.Tracker = unreadable{board: b, cancel: cancel}

	_, err := l.Run(ctx, 42)

	if want := []string{"start 43", "launch 43", "stop 43"}; !errors.Is(err, errUnreadable) || !slices.Equal(b.events, want) {
		t.Fatalf("Run() made %q with error %v; want %q and the read's error", b.events, err, want)
	}
	if last := b.deadlines[len(b.deadlines)-1]; last <= 0 || last > 30*time.Second {
		t.Errorf("Run() unmarked #43 with %v left to run, want a limit of at most 30s", last)
	}
}

func TestRunWarnsOfAFailedUnmarkingAndKeepsTheReadsError(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, refuse: errors.New("HTTP 403")}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Tracker = unreadable{board: b}
	l.Progress = &progress

	_, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not remove the worked mark from #43: HTTP 403"}
	if !errors.Is(err, errUnreadable) || !containsLines(progress.String(), want) {
		t.Errorf("Run() error = %v with progress\n%s\nwant the read's error and the lines %q", err, progress.String(), want)
	}
}

// brittle is progress output that takes its first line and then fails every write, as a closed pipe does.
type brittle struct {
	lines int
}

func (b *brittle) Write(p []byte) (int, error) {
	b.lines++
	if b.lines > 1 {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestRunUnmarksTheTaskWhenReportingItsAgentsFailureFails(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, failing{board: b})
	l.Progress = &brittle{}

	_, err := l.Run(t.Context(), 42)

	if want := []string{"start 43", "launch 43", "stop 43"}; !errors.Is(err, io.ErrClosedPipe) || !slices.Equal(b.events, want) {
		t.Errorf("Run() made %q with error %v; want %q and the progress error", b.events, err, want)
	}
}

func TestRunInteractiveUnmarksTheTaskWhenReportingItsAgentsFailureFails(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, failing{board: b})
	l.Progress = &brittle{}

	_, err := l.RunInteractive(t.Context(), 42)

	if want := []string{"start 43", "launch 43", "stop 43"}; !errors.Is(err, io.ErrClosedPipe) || !slices.Equal(b.events, want) {
		t.Errorf("RunInteractive() made %q with error %v; want %q and the progress error", b.events, err, want)
	}
}
