package loop_test

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func TestAdmitRefusesAnUnavailableSingleTaskNamingWhy(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		task tracker.Task
		want string
	}{
		"closed":          {closed(agentTask(80)), "#80 is closed"},
		"for a human":     {humanTask(80), "#80 is meant for a human; pair on it with --interactive"},
		"for nobody":      {tracker.Task{Number: 80, Open: true, For: tracker.Nobody}, `#80 is not labelled "ready-for-agent"`},
		"blocked":         {agentTask(80, tracker.Blocker{Number: 78}, tracker.Blocker{Number: 79, Open: true}), "#80 is blocked by #79"},
		"blocked by many": {agentTask(80, tracker.Blocker{Number: 79, Open: true}, tracker.Blocker{Repo: "o/x", Number: 3, Open: true}), "#80 is blocked by #79, o/x#3"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := loop.Admit(tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{tc.task}}, tracker.DefaultLabels(), false)

			var refused *loop.RefusedError
			if !errors.As(err, &refused) || err.Error() != tc.want {
				t.Errorf("Admit(#80) = %v, want a RefusedError %q", err, tc.want)
			}
		})
	}
}

func TestAdmitInteractiveRefusesAnUnavailableSingleTaskNamingWhy(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		task tracker.Task
		want string
	}{
		"closed human task":  {closed(humanTask(80)), "#80 is closed"},
		"blocked human task": {blockedBy(humanTask(80), tracker.Blocker{Number: 79, Open: true}), "#80 is blocked by #79"},
		"for nobody":         {tracker.Task{Number: 80, Open: true, For: tracker.Nobody}, `#80 is not labelled "ready-for-agent"`},
		"blocked agent task": {agentTask(80, tracker.Blocker{Number: 79, Open: true}), "#80 is blocked by #79"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := loop.Admit(tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{tc.task}}, tracker.DefaultLabels(), true)

			var refused *loop.RefusedError
			if !errors.As(err, &refused) || err.Error() != tc.want {
				t.Errorf("Admit(#80, interactive) = %v, want a RefusedError %q", err, tc.want)
			}
		})
	}
}

func TestAdmitInteractiveLetsAnOpenUnblockedSingleTaskRunWhoeverItIsFor(t *testing.T) {
	t.Parallel()
	for name, task := range map[string]tracker.Task{
		"for an agent": agentTask(80, tracker.Blocker{Number: 79}),
		"for a human":  blockedBy(humanTask(80), tracker.Blocker{Number: 79}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := loop.Admit(tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{task}}, tracker.DefaultLabels(), true); err != nil {
				t.Errorf("Admit(#80, interactive) = %v, want nil", err)
			}
		})
	}
}

func TestAdmitLetsAnAvailableSingleTaskRun(t *testing.T) {
	t.Parallel()
	task := agentTask(80, tracker.Blocker{Number: 79})

	if err := loop.Admit(tracker.Execution{Number: 74, Single: true, Tasks: []tracker.Task{task}}, tracker.DefaultLabels(), false); err != nil {
		t.Errorf("Admit(#80) = %v, want nil", err)
	}
}

func TestAdmitLetsASpecWithNoAvailableTaskRun(t *testing.T) {
	t.Parallel()
	spec := tracker.Execution{Number: 42, Tasks: []tracker.Task{closed(agentTask(43)), humanTask(44)}}

	if err := loop.Admit(spec, tracker.DefaultLabels(), false); err != nil {
		t.Errorf("Admit(spec #42) = %v, want nil", err)
	}
}

func TestAdmitRefusesASpecWithNoTasks(t *testing.T) {
	t.Parallel()

	err := loop.Admit(tracker.Execution{Number: 76, Title: "Gadgets", Tasks: []tracker.Task{}}, tracker.DefaultLabels(), false)

	var empty *loop.EmptyError
	if !errors.As(err, &empty) || err.Error() != "#76 has no tasks" {
		t.Errorf("Admit(spec #76) = %v, want an EmptyError \"#76 has no tasks\"", err)
	}
}

func TestAdmitNamesTheConfiguredAgentLabelForATaskWithout(t *testing.T) {
	t.Parallel()
	task := tracker.Task{Number: 80, Open: true, For: tracker.Nobody}
	labels := tracker.Labels{Agent: "go", Human: "stop", Spec: "epic"}

	err := loop.Admit(tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{task}}, labels, false)

	want := `#80 is not labelled "go"`
	if err == nil || err.Error() != want {
		t.Errorf("Admit(#80) = %v, want %q", err, want)
	}
}
