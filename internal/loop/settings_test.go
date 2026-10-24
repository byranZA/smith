package loop_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
)

func TestResolveSettingsTakesTheMostSpecificSource(t *testing.T) {
	on, off := true, false
	opus := repofile.File{
		Presets: map[string]repofile.Preset{"opus": {Agent: "claude", Model: "opus", Effort: "high"}},
		Roles:   map[string]string{"work": "opus"},
	}
	tests := []struct {
		name  string
		flags loop.Flags
		file  repofile.File
		want  loop.Settings
	}{
		{
			name: "nothing set anywhere",
			want: loop.Settings{
				Preset: preset.Resolved{
					Name:   "default",
					Origin: config.FromDefault,
					Agent:  config.Value{Value: "claude", Origin: config.FromDefault},
					Model:  config.Value{Origin: config.FromDefault},
					Effort: config.Value{Origin: config.FromDefault},
				},
				Push:       config.Value{Value: "on", Origin: config.FromDefault},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
		{
			name: "the repo file over the default",
			file: repofile.File{Presets: opus.Presets, Roles: opus.Roles, Push: &off},
			want: loop.Settings{
				Preset: preset.Resolved{
					Name:   "opus",
					Origin: config.FromRepoFile,
					Agent:  config.Value{Value: "claude", Origin: config.FromRepoFile},
					Model:  config.Value{Value: "opus", Origin: config.FromRepoFile},
					Effort: config.Value{Value: "high", Origin: config.FromRepoFile},
				},
				Push:       config.Value{Value: "off", Origin: config.FromRepoFile},
				AgentLabel: defaultLabel("ready-for-agent"),
				HumanLabel: defaultLabel("ready-for-human"),
				SpecLabel:  defaultLabel("spec"),
			},
		},
		{
			name:  "flags over the repo file",
			flags: loop.Flags{Preset: preset.Flags{Model: "sonnet"}, Push: &off},
			file:  repofile.File{Presets: opus.Presets, Roles: opus.Roles, Push: &on},
			want: loop.Settings{
				Preset: preset.Resolved{
					Name:   "opus",
					Origin: config.FromRepoFile,
					Agent:  config.Value{Value: "claude", Origin: config.FromRepoFile},
					Model:  config.Value{Value: "sonnet", Origin: config.FromFlag},
					Effort: config.Value{Value: "high", Origin: config.FromRepoFile},
				},
				Push:       config.Value{Value: "off", Origin: config.FromFlag},
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
		flags preset.Flags
		want  string
	}{
		{"an unknown agent as a flag", preset.Flags{Agent: "gemini"}, `--agent: unknown agent "gemini": known agents are claude, codex, pi`},
		{"an effort off the scale as a flag", preset.Flags{Effort: "extreme"}, `--effort: unknown effort "extreme": the scale is low, medium, high`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loop.ResolveSettings(loop.Flags{Preset: tt.flags}, repofile.File{})

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ResolveSettings(%+v) error = %v, want it to contain %q", tt.flags, err, tt.want)
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
			got, err := loop.ResolveSettings(loop.Flags{}, repofile.File{Labels: tt.labels})
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

	_, err := loop.ResolveSettings(loop.Flags{}, file)

	want := `labels.human (repo file) is "ready-for-agent", the name labels.agent (built-in default) has: each label needs a name of its own`
	if err == nil || err.Error() != want {
		t.Errorf("ResolveSettings(labels %+v) error = %v, want %q", file.Labels, err, want)
	}
}

func TestResolveSettingsRefusesALabelNamedForOneOfSmithsOwnMarks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		labels repofile.Labels
		want   string
	}{
		{"agent as working", repofile.Labels{Agent: "smith:working"}, `labels.agent (repo file) is "smith:working", the label smith marks its own work with: name another`},
		{"agent as failed", repofile.Labels{Agent: "smith:failed"}, `labels.agent (repo file) is "smith:failed", the label smith marks its own work with: name another`},
		{"human as working", repofile.Labels{Human: "smith:working"}, `labels.human (repo file) is "smith:working", the label smith marks its own work with: name another`},
		{"human as failed", repofile.Labels{Human: "smith:failed"}, `labels.human (repo file) is "smith:failed", the label smith marks its own work with: name another`},
		{"spec as working", repofile.Labels{Spec: "smith:working"}, `labels.spec (repo file) is "smith:working", the label smith marks its own work with: name another`},
		{"spec as failed", repofile.Labels{Spec: "smith:failed"}, `labels.spec (repo file) is "smith:failed", the label smith marks its own work with: name another`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := loop.ResolveSettings(loop.Flags{}, repofile.File{Labels: tt.labels})

			if err == nil || err.Error() != tt.want {
				t.Errorf("ResolveSettings(labels %+v) error = %v, want %q", tt.labels, err, tt.want)
			}
		})
	}
}
