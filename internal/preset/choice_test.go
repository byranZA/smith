package preset_test

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

// repoFile defines opus (claude, opus, medium) and codex (codex, gpt-6.1-sol,
// high), with the work role on opus.
var repoFile = repofile.File{
	Presets: map[string]repofile.Preset{
		"opus":  {Agent: "claude", Model: "opus", Effort: "medium"},
		"codex": {Agent: "codex", Model: "gpt-6.1-sol", Effort: "high"},
	},
	Roles: map[string]string{"work": "opus"},
}

func fromFile(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromRepoFile}
}

func fromFlag(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromFlag}
}

func fromLabel(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromTaskLabel}
}

func fromDefault(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromDefault}
}

func TestChooseResolvesTheWorkPreset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  repofile.File
		flags preset.Flags
		want  preset.Resolved
	}{
		{
			name: "the work role's preset in the repo file",
			file: repoFile,
			want: preset.Resolved{Name: "opus", Origin: config.FromRepoFile, Agent: fromFile("claude"), Model: fromFile("opus"), Effort: fromFile("medium")},
		},
		{
			name: "no repo file falls back to claude alone",
			want: preset.Resolved{Name: "default", Origin: config.FromDefault, Agent: fromDefault("claude"), Model: fromDefault(""), Effort: fromDefault("")},
		},
		{
			name: "presets without a work role fall back to claude alone",
			file: repofile.File{Presets: repoFile.Presets},
			want: preset.Resolved{Name: "default", Origin: config.FromDefault, Agent: fromDefault("claude"), Model: fromDefault(""), Effort: fromDefault("")},
		},
		{
			name: "a preset need not set a model or effort",
			file: repofile.File{Presets: map[string]repofile.Preset{"plain": {Agent: "codex"}}, Roles: map[string]string{"work": "plain"}},
			want: preset.Resolved{Name: "plain", Origin: config.FromRepoFile, Agent: fromFile("codex"), Model: fromFile(""), Effort: fromFile("")},
		},
		{
			name:  "the agent flag builds a one-off preset without the repo file's model",
			file:  repoFile,
			flags: preset.Flags{Agent: "codex"},
			want:  preset.Resolved{Origin: config.FromFlag, Agent: fromFlag("codex"), Model: fromFlag(""), Effort: fromFlag("")},
		},
		{
			name:  "the agent and model flags build a one-off preset together",
			file:  repoFile,
			flags: preset.Flags{Agent: "pi", Model: "sonnet"},
			want:  preset.Resolved{Origin: config.FromFlag, Agent: fromFlag("pi"), Model: fromFlag("sonnet"), Effort: fromFlag("")},
		},
		{
			name:  "the model flag alone keeps the work preset's agent",
			file:  repoFile,
			flags: preset.Flags{Model: "sonnet"},
			want:  preset.Resolved{Name: "opus", Origin: config.FromRepoFile, Agent: fromFile("claude"), Model: fromFlag("sonnet"), Effort: fromFile("medium")},
		},
		{
			name:  "the model flag alone keeps the default's agent",
			flags: preset.Flags{Model: "sonnet"},
			want:  preset.Resolved{Name: "default", Origin: config.FromDefault, Agent: fromDefault("claude"), Model: fromFlag("sonnet"), Effort: fromDefault("")},
		},
		{
			name:  "the effort flag changes only the work preset's effort",
			file:  repoFile,
			flags: preset.Flags{Effort: "low"},
			want:  preset.Resolved{Name: "opus", Origin: config.FromRepoFile, Agent: fromFile("claude"), Model: fromFile("opus"), Effort: fromFlag("low")},
		},
		{
			name:  "the preset flag picks one of the repo file's presets",
			file:  repoFile,
			flags: preset.Flags{Preset: "codex"},
			want:  preset.Resolved{Name: "codex", Origin: config.FromFlag, Agent: fromFlag("codex"), Model: fromFlag("gpt-6.1-sol"), Effort: fromFlag("high")},
		},
		{
			name:  "the effort flag applies over a pinned preset",
			file:  repoFile,
			flags: preset.Flags{Preset: "codex", Effort: "low"},
			want:  preset.Resolved{Name: "codex", Origin: config.FromFlag, Agent: fromFlag("codex"), Model: fromFlag("gpt-6.1-sol"), Effort: fromFlag("low")},
		},
		{
			name:  "the effort flag applies over a one-off preset",
			file:  repoFile,
			flags: preset.Flags{Agent: "codex", Effort: "low"},
			want:  preset.Resolved{Origin: config.FromFlag, Agent: fromFlag("codex"), Model: fromFlag(""), Effort: fromFlag("low")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			choice, err := preset.Choose(tt.file, tt.flags)
			if err != nil {
				t.Fatalf("Choose(%+v, %+v) error = %v", tt.file, tt.flags, err)
			}

			if got := choice.Work(); got != tt.want {
				t.Errorf("Choose(%+v, %+v).Work() = %+v, want %+v", tt.file, tt.flags, got, tt.want)
			}
		})
	}
}

func TestResolvedOptionsAreItsModelAndEffort(t *testing.T) {
	t.Parallel()
	r := preset.Resolved{Agent: fromFile("claude"), Model: fromFile("opus"), Effort: fromFlag("low")}

	want := agent.Options{Model: "opus", Effort: agent.Low}
	if got := r.Options(); got != want {
		t.Errorf("%+v.Options() = %+v, want %+v", r, got, want)
	}
}

func TestChooseRefusesAnUnknownAgentFlag(t *testing.T) {
	t.Parallel()
	flags := preset.Flags{Agent: "cursor"}

	_, err := preset.Choose(repoFile, flags)

	var unknown *agent.UnknownError
	if !errors.As(err, &unknown) {
		t.Errorf("Choose(repo file, %+v) error = %v, want an *agent.UnknownError", flags, err)
	}
}

func TestChooseRefusesAnEffortFlagOffTheScale(t *testing.T) {
	t.Parallel()
	flags := preset.Flags{Effort: "maximum"}

	_, err := preset.Choose(repoFile, flags)

	var effort *agent.EffortError
	if !errors.As(err, &effort) {
		t.Errorf("Choose(repo file, %+v) error = %v, want an *agent.EffortError", flags, err)
	}
}

func TestChooseRefusesAWorkRoleNamingAnUndefinedPreset(t *testing.T) {
	t.Parallel()
	file := repofile.File{Presets: repoFile.Presets, Roles: map[string]string{"work": "missing"}}

	_, err := preset.Choose(file, preset.Flags{})

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || *undefined != (preset.UndefinedError{Name: "missing", By: "role work"}) {
		t.Errorf("Choose(%+v, no flags) error = %v, want the role work naming undefined preset missing", file, err)
	}
}

func TestChooseRefusesAPresetFlagNamingAnUndefinedPreset(t *testing.T) {
	t.Parallel()
	flags := preset.Flags{Preset: "missing"}

	_, err := preset.Choose(repoFile, flags)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || *undefined != (preset.UndefinedError{Name: "missing", By: "--preset"}) {
		t.Errorf("Choose(repo file, %+v) error = %v, want --preset naming undefined preset missing", flags, err)
	}
}

func TestChooseRefusesThePresetFlagWithTheAgentOrModelFlag(t *testing.T) {
	t.Parallel()
	for _, flags := range []preset.Flags{
		{Preset: "codex", Agent: "claude"},
		{Preset: "codex", Model: "sonnet"},
	} {
		_, err := preset.Choose(repoFile, flags)

		if !errors.Is(err, preset.ErrPresetWithAgent) {
			t.Errorf("Choose(repo file, %+v) error = %v, want %v", flags, err, preset.ErrPresetWithAgent)
		}
	}
}

func TestChoiceTaskResolvesATasksPreset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		flags   preset.Flags
		presets []string
		want    preset.Resolved
	}{
		{
			name: "an unlabelled task runs on the work preset",
			want: preset.Resolved{Name: "opus", Origin: config.FromRepoFile, Agent: fromFile("claude"), Model: fromFile("opus"), Effort: fromFile("medium")},
		},
		{
			name:    "a labelled task runs on its own preset",
			presets: []string{"codex"},
			want:    preset.Resolved{Name: "codex", Origin: config.FromTaskLabel, Agent: fromLabel("codex"), Model: fromLabel("gpt-6.1-sol"), Effort: fromLabel("high")},
		},
		{
			name:    "the effort flag alone keeps the task's preset",
			flags:   preset.Flags{Effort: "low"},
			presets: []string{"codex"},
			want:    preset.Resolved{Name: "codex", Origin: config.FromTaskLabel, Agent: fromLabel("codex"), Model: fromLabel("gpt-6.1-sol"), Effort: fromFlag("low")},
		},
		{
			name:    "the preset flag pins the task over its label",
			flags:   preset.Flags{Preset: "opus"},
			presets: []string{"codex"},
			want:    preset.Resolved{Name: "opus", Origin: config.FromFlag, Agent: fromFlag("claude"), Model: fromFlag("opus"), Effort: fromFlag("medium")},
		},
		{
			name:    "the agent flag pins the task over its label",
			flags:   preset.Flags{Agent: "pi"},
			presets: []string{"codex"},
			want:    preset.Resolved{Origin: config.FromFlag, Agent: fromFlag("pi"), Model: fromFlag(""), Effort: fromFlag("")},
		},
		{
			name:    "the model flag pins the task to the work preset over its label",
			flags:   preset.Flags{Model: "sonnet"},
			presets: []string{"codex"},
			want:    preset.Resolved{Name: "opus", Origin: config.FromRepoFile, Agent: fromFile("claude"), Model: fromFlag("sonnet"), Effort: fromFile("medium")},
		},
		{
			name:    "a pinned run ignores a label the repo file does not define",
			flags:   preset.Flags{Preset: "codex"},
			presets: []string{"missing", "opus"},
			want:    preset.Resolved{Name: "codex", Origin: config.FromFlag, Agent: fromFlag("codex"), Model: fromFlag("gpt-6.1-sol"), Effort: fromFlag("high")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			choice, err := preset.Choose(repoFile, tt.flags)
			if err != nil {
				t.Fatalf("Choose(repo file, %+v) error = %v", tt.flags, err)
			}
			task := tracker.Task{Number: 2, Open: true, Presets: tt.presets}

			got, err := choice.Task(task)
			if err != nil {
				t.Fatalf("Choose(repo file, %+v).Task(%+v) error = %v", tt.flags, task, err)
			}
			if got != tt.want {
				t.Errorf("Choose(repo file, %+v).Task(%+v) = %+v, want %+v", tt.flags, task, got, tt.want)
			}
		})
	}
}

func TestChoiceTaskRefusesALabelNamingAnUndefinedPreset(t *testing.T) {
	t.Parallel()
	choice, err := preset.Choose(repoFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(repo file, no flags) error = %v", err)
	}
	task := tracker.Task{Number: 3, Open: true, Presets: []string{"missing"}}

	_, err = choice.Task(task)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || *undefined != (preset.UndefinedError{Name: "missing", By: "#3's label preset:missing"}) {
		t.Errorf("Task(%+v) error = %v, want #3's label naming undefined preset missing", task, err)
	}
}

func TestChoiceTaskRefusesTwoPresetLabels(t *testing.T) {
	t.Parallel()
	choice, err := preset.Choose(repoFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(repo file, no flags) error = %v", err)
	}
	task := tracker.Task{Number: 3, Open: true, Presets: []string{"opus", "codex"}}

	_, err = choice.Task(task)

	const want = "#3 carries the labels preset:opus and preset:codex: a task runs on one preset"
	if err == nil || err.Error() != want {
		t.Errorf("Task(%+v) error = %v, want %q", task, err, want)
	}
}

func TestChoiceCheckRefusesAnOpenTaskWhosePresetDoesNotResolve(t *testing.T) {
	t.Parallel()
	choice, err := preset.Choose(repoFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(repo file, no flags) error = %v", err)
	}
	execution := tracker.Execution{Number: 1, Tasks: []tracker.Task{
		{Number: 2, Open: true},
		{Number: 3, Open: true, Presets: []string{"missing"}},
	}}

	err = choice.Check(execution)

	var undefined *preset.UndefinedError
	if !errors.As(err, &undefined) || *undefined != (preset.UndefinedError{Name: "missing", By: "#3's label preset:missing"}) {
		t.Errorf("Check(%+v) error = %v, want #3's label naming undefined preset missing", execution, err)
	}
}

func TestChoiceCheckPassesOverAClosedTasksLabel(t *testing.T) {
	t.Parallel()
	choice, err := preset.Choose(repoFile, preset.Flags{})
	if err != nil {
		t.Fatalf("Choose(repo file, no flags) error = %v", err)
	}
	execution := tracker.Execution{Number: 1, Tasks: []tracker.Task{
		{Number: 2, Presets: []string{"missing"}},
		{Number: 3, Open: true, Presets: []string{"codex"}},
	}}

	if err := choice.Check(execution); err != nil {
		t.Errorf("Check(%+v) error = %v, want none", execution, err)
	}
}

func TestChoiceCheckOfAPinnedRunPassesOverEveryLabel(t *testing.T) {
	t.Parallel()
	flags := preset.Flags{Agent: "codex"}
	choice, err := preset.Choose(repoFile, flags)
	if err != nil {
		t.Fatalf("Choose(repo file, %+v) error = %v", flags, err)
	}
	execution := tracker.Execution{Number: 1, Tasks: []tracker.Task{
		{Number: 2, Open: true, Presets: []string{"missing"}},
		{Number: 3, Open: true, Presets: []string{"opus", "codex"}},
	}}

	if err := choice.Check(execution); err != nil {
		t.Errorf("Check(%+v) error = %v, want none", execution, err)
	}
}
