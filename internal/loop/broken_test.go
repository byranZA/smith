package loop_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// The ways a scripted agent can end: a non-zero exit, a SIGKILL, and a launch that never started it.
var (
	errExit       = errors.New("run claude: exit status 1")
	errKilled     = fmt.Errorf("run claude: %w: signal: killed", loop.ErrKilled)
	errNotStarted = fmt.Errorf("run claude: %w: exec: \"claude\": executable file not found in $PATH", loop.ErrNotStarted)
)

// scripted is a launcher whose agent does its work on the board and then ends each run as exits says in turn, cleanly once they run out.
type scripted struct {
	board *board
	exits []error
}

func (s *scripted) Launch(ctx context.Context, cmd agent.Command) error {
	if err := s.board.Launch(ctx, cmd); err != nil {
		return err
	}
	if len(s.exits) == 0 {
		return nil
	}
	exit := s.exits[0]
	s.exits = s.exits[1:]
	return exit
}

func TestRunStopsAsAgentBrokenWhenEveryAttemptAtATaskIsAnAgentFailure(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43, 44}}
	r := &recorder{}
	l := loopOn(t, b, failing{board: b})
	l.Recorder = r

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := []string{"start 43", "launch 43", "stop 43", "start 43", "launch 43", "stop 43"}
	if want := []loop.Ending{loop.AgentBroken}; !slices.Equal(b.events, events) || !reflect.DeepEqual(r.endings, want) || len(commentsOn(b, 43)) != 0 {
		t.Errorf("Run() made %q, recorded %q and commented %q on #43; want %q, %q and no comment", b.events, r.endings, commentsOn(b, 43), events, want)
	}
}

func TestRunSkipsATaskWhoseAgentRanCleanlyOnOneAttempt(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}

	outcome, err := loopOn(t, b, &scripted{board: b, exits: []error{errExit, nil}}).Run(t.Context(), 42)

	if err != nil || !slices.Equal(b.handed(), []string{"43", "43", "44"}) || !slices.Equal(numbers(outcome.Remaining.Skipped), []int{43}) || outcome.Broken != nil {
		t.Errorf("Run() = %+v, %v, handed %v; want #43 twice then skipped, and #44 worked", outcome, err, b.handed())
	}
}

func TestRunStopsAsAgentBrokenOnTheFirstLaunchThatCannotStartTheAgent(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43, 44}}
	r := &recorder{}
	l := loopOn(t, b, &scripted{board: b, exits: []error{errNotStarted, errNotStarted, errNotStarted, errNotStarted}})
	l.Recorder = r

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := []string{"start 43", "launch 43", "stop 43"}
	if want := []loop.Ending{loop.AgentBroken}; !slices.Equal(b.events, events) || !reflect.DeepEqual(r.endings, want) {
		t.Errorf("Run() made %q and recorded %q; want %q and %q", b.events, r.endings, events, want)
	}
}

func TestAnAgentBrokenRunPostsItsStopReportNamingTheTaskTheErrorAndTheMachine(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43, 44}}
	r := &recorder{}
	l := loopOn(t, b, &scripted{board: b, exits: []error{errExit, errExit}})
	l.Recorder = r
	l.Hostname = hostnamed("box-1")

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	report := "spec #42 stopped: the agent failed on every attempt at #43 (run claude: exit status 1); check that the agent runs on box-1\n" +
		"  available: #43, #44\n" +
		"work branch: smith/42-spec\n"
	want := []string{"smith stopped its run of this spec: **agent broken**.\n\n```text\n" + report + "```\n"}
	if got := commentsOn(b, 42); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(r.reports, []string{report}) {
		t.Errorf("Run() commented %q and recorded %q, want %q and %q", got, r.reports, want, report)
	}
}

func TestAnAgentBrokenRunOnAnAgentThatCannotStartSaysSo(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	r := &recorder{}
	l := loopOn(t, b, &scripted{board: b, exits: []error{errNotStarted}})
	l.Recorder = r
	l.Hostname = hostnamed("box-1")

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"spec #42 stopped: the agent could not be started on #43 (run claude: agent not started: exec: \"claude\": executable file not found in $PATH); check that the agent runs on box-1\n" +
		"  available: #43\n" +
		"work branch: smith/42-spec\n"}
	if !reflect.DeepEqual(r.reports, want) {
		t.Errorf("Run() recorded %q, want %q", r.reports, want)
	}
}

func TestAnAgentBrokenRunWarnsOfAHostnameItCannotReadAndMarksItUnknown(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	var progress strings.Builder
	l := loopOn(t, b, failing{board: b})
	l.Progress = &progress
	l.Hostname = func() (string, error) { return "", errors.New("no hostname") }

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not read this machine's hostname: no hostname"}
	if err != nil || outcome.Broken == nil || outcome.Broken.Machine != "unknown" || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant the machine unknown and the lines %q", outcome, err, progress.String(), want)
	}
}

// failsOn is a launcher over a journal whose agent exits non-zero, doing nothing, on each task in tasks.
type failsOn struct {
	journal *journal
	tasks   []int
}

func (f failsOn) Launch(ctx context.Context, cmd agent.Command) error {
	n, err := strconv.Atoi(taskOf(cmd))
	if err != nil {
		return err
	}
	if slices.Contains(f.tasks, n) {
		f.journal.events = append(f.journal.events, "fail "+taskOf(cmd))
		return errExit
	}
	return f.journal.Launch(ctx, cmd)
}

func TestAnAgentBrokenRunKeepsTheTasksItClosedLandedAndPushed(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}}
	l := journalLoop(t, j)
	l.Launcher = failsOn{journal: j, tasks: []int{44}}

	outcome, err := l.Run(t.Context(), 42)

	want := []string{
		"open 43", "hand 43 in /bench/43", "land 43", "push smith/42-spec",
		"open 44", "fail 44", "open 44", "fail 44",
	}
	if err != nil || outcome.Broken == nil || !slices.Equal(j.events, want) {
		t.Errorf("Run() = %+v, %v with events %q, want the run stopped as agent broken after %q", outcome, err, j.events, want)
	}
}
