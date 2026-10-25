package loop_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/tracker"
)

func TestAgentsNamesTheAgentsARunHandsTasksTo(t *testing.T) {
	blocker := tracker.Blocker{Number: 43, Open: true}
	tests := []struct {
		name        string
		tasks       []tracker.Task
		flags       preset.Flags
		interactive bool
		want        []string
	}{
		{"unlabelled tasks run on the work preset", []tracker.Task{agentTask(43), agentTask(44)}, preset.Flags{}, false, []string{"claude"}},
		{"a labelled task alone needs only its own agent", []tracker.Task{labelled(43, "codex")}, preset.Flags{}, false, []string{"codex"}},
		{"each agent once, in task order", []tracker.Task{labelled(43, "codex"), agentTask(44), labelled(45, "codex")}, preset.Flags{}, false, []string{"codex", "claude"}},
		{"a blocked task is still handed out later", []tracker.Task{agentTask(43), blockedBy(labelled(44, "codex"), blocker)}, preset.Flags{}, false, []string{"claude", "codex"}},
		{"closed and human tasks need no agent", []tracker.Task{closed(labelled(43, "codex")), humanTask(44), agentTask(45)}, preset.Flags{}, false, []string{"claude"}},
		{"a pinned run needs only the pinned agent", []tracker.Task{agentTask(43), labelled(44, "opus")}, preset.Flags{Preset: "codex"}, false, []string{"codex"}},
		{"interactive needs only the next task's agent", []tracker.Task{labelled(43, "codex"), agentTask(44)}, preset.Flags{}, true, []string{"codex"}},
		{"interactive skips a blocked task", []tracker.Task{blockedBy(labelled(43, "codex"), blocker), agentTask(44)}, preset.Flags{}, true, []string{"claude"}},
		{"interactive with nothing available needs no agent", []tracker.Task{humanTask(43), agentTask(44, blocker)}, preset.Flags{}, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			choice, err := preset.Choose(presetFile, tc.flags)
			if err != nil {
				t.Fatalf("Choose(preset file, %+v) error = %v", tc.flags, err)
			}

			got, err := loop.Agents(choice, tracker.Execution{Tasks: tc.tasks}, tc.interactive)

			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Agents() = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestAgentsInteractiveOnANamedHumanTaskNamesItsAgent(t *testing.T) {
	choice, err := preset.Choose(presetFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(preset file) error = %v", err)
	}
	task := labelled(80, "codex")
	task.For = tracker.Human

	got, err := loop.Agents(choice, tracker.Execution{Number: 80, Single: true, Tasks: []tracker.Task{task}}, true)

	if want := []string{"codex"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Agents(#80, interactive) = %v, %v; want %v", got, err, want)
	}
}

func TestAgentsRefusesATaskWhosePresetDoesNotResolve(t *testing.T) {
	choice, err := preset.Choose(presetFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(preset file) error = %v", err)
	}

	_, err = loop.Agents(choice, tracker.Execution{Tasks: []tracker.Task{agentTask(43), labelled(44, "missing")}}, false)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || undefined.Name != "missing" {
		t.Errorf("Agents() error = %v; want preset \"missing\" undefined", err)
	}
}

// blockedBy is task blocked by blocker.
func blockedBy(task tracker.Task, blocker tracker.Blocker) tracker.Task {
	task.Blockers = append(task.Blockers, blocker)
	return task
}
