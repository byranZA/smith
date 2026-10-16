package loop_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/repofile"
)

func TestResolveSettingsTakesTheMostSpecificSource(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name  string
		flags repofile.File
		file  repofile.File
		want  loop.Settings
	}{
		{
			name: "nothing set anywhere",
			want: loop.Settings{
				Agent:      config.Value{Value: "claude", Origin: config.FromDefault},
				Model:      config.Value{Origin: config.FromDefault},
				Effort:     config.Value{Origin: config.FromDefault},
				Push:       config.Value{Value: "on", Origin: config.FromDefault},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
		{
			name: "the repo file over the default",
			file: repofile.File{Agent: "claude", Model: "opus", Effort: "high", Push: &off},
			want: loop.Settings{
				Agent:      config.Value{Value: "claude", Origin: config.FromRepoFile},
				Model:      config.Value{Value: "opus", Origin: config.FromRepoFile},
				Effort:     config.Value{Value: "high", Origin: config.FromRepoFile},
				Push:       config.Value{Value: "off", Origin: config.FromRepoFile},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
		{
			name:  "a flag over the repo file, field by field",
			flags: repofile.File{Model: "sonnet", Push: &off},
			file:  repofile.File{Model: "opus", Effort: "high", Push: &on},
			want: loop.Settings{
				Agent:      config.Value{Value: "claude", Origin: config.FromDefault},
				Model:      config.Value{Value: "sonnet", Origin: config.FromFlag},
				Effort:     config.Value{Value: "high", Origin: config.FromRepoFile},
				Push:       config.Value{Value: "off", Origin: config.FromFlag},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
		{
			name:  "a flag over the default",
			flags: repofile.File{Agent: "claude", Effort: "low"},
			want: loop.Settings{
				Agent:      config.Value{Value: "claude", Origin: config.FromFlag},
				Model:      config.Value{Origin: config.FromDefault},
				Effort:     config.Value{Value: "low", Origin: config.FromFlag},
				Push:       config.Value{Value: "on", Origin: config.FromDefault},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loop.ResolveSettings(tt.flags, tt.file)

			if err != nil || got != tt.want {
				t.Errorf("ResolveSettings() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestResolveSettingsRefusesWhatNoAgentCanRun(t *testing.T) {
	tests := []struct {
		name  string
		flags repofile.File
		file  repofile.File
		want  string
	}{
		{"an unknown agent in the repo file", repofile.File{}, repofile.File{Agent: "gemini"}, `agent (repo file): unknown agent "gemini": known agents are claude, codex, pi`},
		{"an unknown agent as a flag", repofile.File{Agent: "gemini"}, repofile.File{}, `agent (flag): unknown agent "gemini"`},
		{"an effort off the scale as a flag", repofile.File{Effort: "extreme"}, repofile.File{}, `effort (flag): unknown effort "extreme": the scale is low, medium, high`},
		{"an effort off the scale in the repo file", repofile.File{}, repofile.File{Effort: "max"}, `effort (repo file): unknown effort "max"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loop.ResolveSettings(tt.flags, tt.file)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ResolveSettings() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// defaultLabel is a triage label resolved to its built-in default.
func defaultLabel(name string) config.Value {
	return config.Value{Value: name, Origin: config.FromDefault}
}

// fileLabel is a triage label resolved from the repo file.
func fileLabel(name string) config.Value {
	return config.Value{Value: name, Origin: config.FromRepoFile}
}

func TestResolveSettingsTakesEachLabelFromTheRepoFileOverItsDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		labels repofile.Labels
		want   [3]config.Value
	}{
		{"all three", repofile.Labels{Agent: "go", Human: "stop", Spec: "epic"}, [3]config.Value{fileLabel("go"), fileLabel("stop"), fileLabel("epic")}},
		{"the agent label alone", repofile.Labels{Agent: "go"}, [3]config.Value{fileLabel("go"), defaultLabel("ready-for-human"), defaultLabel("spec")}},
		{"the human label alone", repofile.Labels{Human: "stop"}, [3]config.Value{defaultLabel("ready-for-agent"), fileLabel("stop"), defaultLabel("spec")}},
		{"the spec label alone", repofile.Labels{Spec: "epic"}, [3]config.Value{defaultLabel("ready-for-agent"), defaultLabel("ready-for-human"), fileLabel("epic")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := loop.ResolveSettings(repofile.File{}, repofile.File{Labels: tt.labels})
			if err != nil {
				t.Fatalf("ResolveSettings(labels %+v) error = %v", tt.labels, err)
			}

			if labels := [3]config.Value{got.AgentLabel, got.HumanLabel, got.SpecLabel}; labels != tt.want {
				t.Errorf("ResolveSettings(labels %+v) labels = %+v, want %+v", tt.labels, labels, tt.want)
			}
		})
	}
}

func TestResolveSettingsRefusesALabelSharingADefaultsName(t *testing.T) {
	t.Parallel()
	file := repofile.File{Labels: repofile.Labels{Human: "ready-for-agent"}}

	_, err := loop.ResolveSettings(repofile.File{}, file)

	want := `labels.human (repo file) is "ready-for-agent", the name labels.agent (built-in default) has: each label needs a name of its own`
	if err == nil || err.Error() != want {
		t.Errorf("ResolveSettings(labels %+v) error = %v, want %q", file.Labels, err, want)
	}
}
