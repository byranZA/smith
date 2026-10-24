package loop_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

// presetFile defines opus (claude, opus) and codex (codex, gpt-6.1-sol), with the work role on opus.
var presetFile = repofile.File{
	Presets: map[string]repofile.Preset{
		"opus":  {Agent: "claude", Model: "opus"},
		"codex": {Agent: "codex", Model: "gpt-6.1-sol"},
	},
	Roles: map[string]string{"work": "opus"},
}

// loopWith is loopOn choosing each task's preset from presetFile under flags.
func loopWith(t *testing.T, b *board, flags preset.Flags) loop.Loop {
	t.Helper()
	choice, err := preset.Choose(presetFile, flags)
	if err != nil {
		t.Fatalf("Choose(preset file, %+v) error = %v", flags, err)
	}
	l := loopOn(t, b, b)
	l.Presets = choice
	return l
}

// labelled is an agent task carrying a preset: label for each of presets.
func labelled(n int, presets ...string) tracker.Task {
	task := agentTask(n)
	task.Presets = presets
	return task
}

// launches renders each launched command as its name and args.
func launches(b *board) [][]string {
	var got [][]string
	for _, cmd := range b.launched {
		got = append(got, append([]string{cmd.Name}, cmd.Args...))
	}
	return got
}

func TestRunHandsEachTaskToItsOwnPreset(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), labelled(44, "codex")}}

	if _, err := loopWith(t, b, preset.Flags{}).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := [][]string{
		{"claude", "--permission-mode", "auto", "--model", "opus", "--print", "43"},
		{"codex", "exec", "--yolo", "--model", "gpt-6.1-sol", "44"},
	}
	if got := launches(b); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Run() launched %q, want %q", got, want)
	}
}

func TestRunWithAPinnedPresetHandsEveryTaskToIt(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), labelled(44, "missing")}}

	if _, err := loopWith(t, b, preset.Flags{Agent: "codex"}).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := [][]string{
		{"codex", "exec", "--yolo", "43"},
		{"codex", "exec", "--yolo", "44"},
	}
	if got := launches(b); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Run() launched %q, want %q", got, want)
	}
}

func TestRunWithTheEffortFlagKeepsEachTasksPreset(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), labelled(44, "codex")}}

	if _, err := loopWith(t, b, preset.Flags{Effort: "low"}).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := [][]string{
		{"claude", "--permission-mode", "auto", "--model", "opus", "--effort", "low", "--print", "43"},
		{"codex", "exec", "--yolo", "--model", "gpt-6.1-sol", "--config", `model_reasoning_effort="low"`, "44"},
	}
	if got := launches(b); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Run() launched %q, want %q", got, want)
	}
}

func TestRunStopsBeforeAnyAgentOnATaskNamingAnUndefinedPreset(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44), labelled(45, "missing")}}

	_, err := loopWith(t, b, preset.Flags{}).Run(t.Context(), 42)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || *undefined != (preset.UndefinedError{Name: "missing", By: "#45's label preset:missing"}) || len(b.launched) != 0 {
		t.Errorf("Run() = %v, launched %v; want #45's undefined preset missing refused with no agent launched", err, b.launched)
	}
}

func TestRunStopsBeforeAnyAgentOnATaskWithTwoPresetLabels(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), labelled(44, "opus", "codex")}}

	_, err := loopWith(t, b, preset.Flags{}).Run(t.Context(), 42)

	var labels *preset.LabelsError
	if !errors.As(err, &labels) || labels.Task != 44 || !slices.Equal(labels.Presets, []string{"opus", "codex"}) || len(b.launched) != 0 {
		t.Errorf("Run() = %v, launched %v; want #44's two preset labels refused with no agent launched", err, b.launched)
	}
}

func TestRunPassesOverAClosedTasksPresetLabel(t *testing.T) {
	closed := labelled(43, "missing")
	closed.Open = false
	b := &board{tasks: []tracker.Task{closed, agentTask(44)}}

	if _, err := loopWith(t, b, preset.Flags{}).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := [][]string{{"claude", "--permission-mode", "auto", "--model", "opus", "--print", "44"}}
	if got := launches(b); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Run() launched %q, want %q", got, want)
	}
}

func TestRunInteractiveOpensALabelledTaskOnItsOwnPreset(t *testing.T) {
	b := &board{tasks: []tracker.Task{labelled(43, "codex")}}

	if _, err := loopWith(t, b, preset.Flags{}).RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if len(b.launched) != 1 || b.launched[0].Name != "codex" || !b.launched[0].Attached || !slices.Equal(b.launched[0].Args[:2], []string{"--model", "gpt-6.1-sol"}) {
		t.Errorf("RunInteractive() launched %+v, want codex attached with model gpt-6.1-sol", b.launched)
	}
}

func TestRunInteractiveStopsBeforeAnyAgentOnATaskNamingAnUndefinedPreset(t *testing.T) {
	b := &board{tasks: []tracker.Task{labelled(43, "missing")}}

	_, err := loopWith(t, b, preset.Flags{}).RunInteractive(t.Context(), 42)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || len(b.launched) != 0 {
		t.Errorf("RunInteractive() = %v, launched %v; want the undefined preset refused with no agent launched", err, b.launched)
	}
}
