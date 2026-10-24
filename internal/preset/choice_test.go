package preset_test

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
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
