package loop_test

import (
	"context"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// Ways task #80 can stop being a task during an agent's attempt at it.
const (
	staysATask = iota
	gainsAChild
	gainsTheSpecLabel
)

// sprouting is a tracker, launcher, bench and pusher for #80 named on its own, which the agent's attempt changes as becomes says, closing it unless leaveOpen.
type sprouting struct {
	becomes   int
	leaveOpen bool
	changed   bool
	open      bool
	events    []string
	handed    []string
}

func (s *sprouting) Execution(_ context.Context, number int) (tracker.Execution, error) {
	switch {
	case !s.changed || s.becomes == staysATask:
		return tracker.Execution{Number: number, Title: "Fix X", Single: true, Tasks: []tracker.Task{s.task()}}, nil
	case s.becomes == gainsAChild:
		return tracker.Execution{Number: number, Title: "Fix X", Tasks: []tracker.Task{agentTask(81)}}, nil
	default:
		return tracker.Execution{Number: number, Title: "Fix X", Tasks: []tracker.Task{}}, nil
	}
}

func (s *sprouting) Task(context.Context, int) (tracker.Task, error) { return s.task(), nil }

func (s *sprouting) Start(context.Context, int) error { return nil }

func (s *sprouting) Stop(context.Context, int) error { return nil }

func (s *sprouting) Fail(context.Context, int) error { return nil }

func (s *sprouting) Comment(context.Context, int, string) error { return nil }

func (s *sprouting) task() tracker.Task {
	task := tracker.Task{Number: 80, Title: "Fix X", Open: s.open, For: tracker.Agent}
	if s.changed && s.becomes != staysATask {
		task.For = tracker.Nobody
	}
	return task
}

func (s *sprouting) Launch(_ context.Context, cmd agent.Command) error {
	s.handed = append(s.handed, taskOf(cmd))
	s.events = append(s.events, "hand "+taskOf(cmd))
	s.changed = true
	s.open = s.leaveOpen
	return nil
}

func (s *sprouting) WorkBranch() string { return "smith/80-fix-x" }

func (s *sprouting) TaskBranch(int) string { return "smith/tasks/smith/80-fix-x/80" }

func (s *sprouting) Ahead(context.Context, int) (int, error) { return 0, nil }

func (s *sprouting) Open(context.Context, int) (string, error) { return "/bench/80", nil }

func (s *sprouting) Land(context.Context, int) error {
	s.events = append(s.events, "land")
	return nil
}

func (s *sprouting) Push(_ context.Context, branch string) error {
	s.events = append(s.events, "push "+branch)
	return nil
}

func sproutingLoop(t *testing.T, s *sprouting) loop.Loop {
	t.Helper()
	l := loopOn(t, &board{}, s)
	l.Tracker, l.Bench, l.Pusher = s, s, s
	return l
}

func TestRunLandsANamedTaskThatClosedWhileBecomingASpec(t *testing.T) {
	t.Parallel()
	for name, becomes := range map[string]int{"gaining a child": gainsAChild, "gaining the spec label": gainsTheSpecLabel} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &sprouting{becomes: becomes, open: true}

			outcome, err := sproutingLoop(t, s).Run(t.Context(), 80)

			want := []string{"hand 80", "land", "push smith/80-fix-x"}
			if err != nil || !outcome.Complete() || !slices.Equal(s.events, want) {
				t.Errorf("Run(80) = %+v, %v, events %q; want it complete with events %q", outcome, err, s.events, want)
			}
		})
	}
}

func TestRunOfANamedTaskLeftOpenEndsIncompleteWithoutWorkingAnythingElse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		becomes int
		handed  []string
	}{
		"staying a task":  {staysATask, []string{"80", "80"}},
		"gaining a child": {gainsAChild, []string{"80"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &sprouting{becomes: tc.becomes, open: true, leaveOpen: true}

			outcome, err := sproutingLoop(t, s).Run(t.Context(), 80)

			if err != nil || outcome.Complete() || !slices.Equal(s.handed, tc.handed) {
				t.Errorf("Run(80) = %+v, %v, handed %v; want it incomplete after handing %v", outcome, err, s.handed, tc.handed)
			}
		})
	}
}

func TestRunInteractiveLandsANamedTaskThatClosedWhileGainingAChild(t *testing.T) {
	t.Parallel()
	s := &sprouting{becomes: gainsAChild, open: true}

	outcome, err := sproutingLoop(t, s).RunInteractive(t.Context(), 80)

	want := []string{"hand 80", "land", "push smith/80-fix-x"}
	if err != nil || !outcome.Succeeded() || !slices.Equal(s.events, want) {
		t.Errorf("RunInteractive(80) = %+v, %v, events %q; want it succeeded with events %q", outcome, err, s.events, want)
	}
}
