package loop_test

import (
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func agentTask(n int, blockers ...tracker.Blocker) tracker.Task {
	return tracker.Task{Number: n, Title: "task", Open: true, For: tracker.Agent, Blockers: blockers}
}

func humanTask(n int) tracker.Task {
	return tracker.Task{Number: n, Title: "task", Open: true, For: tracker.Human}
}

func closed(task tracker.Task) tracker.Task {
	task.Open = false
	return task
}

func numbers(tasks []tracker.Task) []int {
	var ns []int
	for _, task := range tasks {
		ns = append(ns, task.Number)
	}
	return ns
}

func TestNextIsTheFirstAvailableTask(t *testing.T) {
	for name, tc := range map[string]struct {
		tasks []tracker.Task
		want  int
	}{
		"first of several":       {[]tracker.Task{agentTask(43), agentTask(44), agentTask(45)}, 43},
		"closed passed over":     {[]tracker.Task{closed(agentTask(43)), agentTask(44)}, 44},
		"blocked passed over":    {[]tracker.Task{humanTask(43), agentTask(44, tracker.Blocker{Number: 43, Open: true}), agentTask(45)}, 45},
		"blockers all closed":    {[]tracker.Task{closed(agentTask(43)), agentTask(44, tracker.Blocker{Number: 43})}, 44},
		"fix task filed mid-run": {[]tracker.Task{closed(agentTask(43)), closed(agentTask(44)), closed(agentTask(45)), agentTask(50)}, 50},
		"in listed order":        {[]tracker.Task{agentTask(45), agentTask(43)}, 45},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := loop.Survey(tracker.Execution{Number: 42, Tasks: tc.tasks}).Next()

			if !ok || got.Number != tc.want {
				t.Errorf("Next() = #%d, %v, want #%d", got.Number, ok, tc.want)
			}
		})
	}
}

func TestNoTaskIsNextWhenOnlyAHumanCanProceed(t *testing.T) {
	remaining := loop.Survey(tracker.Execution{Number: 42, Tasks: []tracker.Task{humanTask(43), closed(agentTask(44)), closed(agentTask(45))}})

	if got, ok := remaining.Next(); ok {
		t.Errorf("Next() = #%d, want no next task", got.Number)
	}
}

func TestSurveySortsTheTasksByWhereTheyStand(t *testing.T) {
	unready := tracker.Task{Number: 47, Open: true, For: tracker.Nobody}
	spec := tracker.Execution{Number: 42, Tasks: []tracker.Task{
		humanTask(43),
		agentTask(44, tracker.Blocker{Number: 43, Open: true}),
		closed(agentTask(45)),
		agentTask(46),
		unready,
		closed(humanTask(48)),
	}}

	got := loop.Survey(spec)

	want := map[string][]int{"available": {46}, "blocked": {44}, "human": {43}, "unready": {47}, "closed": {45, 48}}
	gotNumbers := map[string][]int{"available": numbers(got.Available), "blocked": numbers(got.Blocked), "human": numbers(got.Human), "unready": numbers(got.Unready), "closed": numbers(got.Closed)}
	if !reflect.DeepEqual(gotNumbers, want) {
		t.Errorf("Survey() = %v, want %v", gotNumbers, want)
	}
}

func TestCompleteOnlyWhenEveryTaskIsClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		tasks []tracker.Task
		want  bool
	}{
		"all closed":         {[]tracker.Task{closed(agentTask(43)), closed(humanTask(44))}, true},
		"no tasks":           {nil, true},
		"a human task open":  {[]tracker.Task{closed(agentTask(43)), humanTask(44)}, false},
		"an agent task open": {[]tracker.Task{agentTask(43)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := loop.Survey(tracker.Execution{Number: 42, Tasks: tc.tasks}).Complete(); got != tc.want {
				t.Errorf("Complete() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPairingHandsOutANamedTaskThatIsOpenUnblockedAndLabelled(t *testing.T) {
	t.Parallel()
	blocker := tracker.Blocker{Number: 79, Open: true}
	for name, tc := range map[string]struct {
		task   tracker.Task
		want   int
		wantOK bool
	}{
		"for a human":  {humanTask(80), 80, true},
		"for an agent": {agentTask(80, tracker.Blocker{Number: 79}), 80, true},
		"closed":       {closed(humanTask(80)), 0, false},
		"blocked":      {blockedBy(humanTask(80), blocker), 0, false},
		"unlabelled":   {tracker.Task{Number: 80, Open: true, For: tracker.Nobody}, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := loop.Pairing(tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{tc.task}})

			if got.Number != tc.want || ok != tc.wantOK {
				t.Errorf("Pairing(#80) = #%d, %v; want #%d, %v", got.Number, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestPairingOnASpecHandsOutItsFirstTaskForAnAgent(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tasks  []tracker.Task
		want   int
		wantOK bool
	}{
		"human task skipped":  {[]tracker.Task{humanTask(80), agentTask(81)}, 81, true},
		"only a human task":   {[]tracker.Task{humanTask(80)}, 0, false},
		"blocked task passed": {[]tracker.Task{agentTask(80, tracker.Blocker{Number: 79, Open: true}), agentTask(81)}, 81, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := loop.Pairing(tracker.Execution{Number: 74, Tasks: tc.tasks})

			if got.Number != tc.want || ok != tc.wantOK {
				t.Errorf("Pairing(spec #74) = #%d, %v; want #%d, %v", got.Number, ok, tc.want, tc.wantOK)
			}
		})
	}
}
