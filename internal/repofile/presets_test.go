package repofile_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/repofile"
)

func TestParseReadsPresetsAndRoles(t *testing.T) {
	t.Parallel()
	data := "presets:\n  opus: {agent: claude, model: opus, effort: medium}\n  plain: {agent: codex}\nroles:\n  work: opus\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{
		Presets: map[string]repofile.Preset{
			"opus":  {Agent: "claude", Model: "opus", Effort: "medium"},
			"plain": {Agent: "codex"},
		},
		Roles: map[string]string{"work": "opus"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAFlatAgentKeyNamingItsLineAndThePresetsShape(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"agent", "model", "effort"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			data := "push: false\n" + key + ": high\n"

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{
				Line:    2,
				Path:    key,
				Message: key + " is set in a preset now, not at the top level: name the agent, model and effort as a preset and point the work role at it, as in presets: {opus: {agent: claude, model: opus, effort: high}} and roles: {work: opus}",
			}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseValidatesEveryPresetWhetherOrNotItIsUsed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, preset string
		want         []blueprint.Finding
	}{
		{"valid", "fast: {agent: claude, effort: low}", nil},
		{"unknown agent", "fast: {agent: cursor, effort: low}", []blueprint.Finding{
			{Line: 3, Path: "presets.fast.agent", Message: `unknown agent "cursor": known agents are claude, codex, pi`},
		}},
		{"effort off the scale", "fast: {agent: claude, effort: maximum}", []blueprint.Finding{
			{Line: 3, Path: "presets.fast.effort", Message: `unknown effort "maximum": the scale is low, medium, high`},
		}},
		{"uppercase name", "Fast: {agent: claude, effort: low}", []blueprint.Finding{
			{Line: 3, Path: "presets.Fast", Message: `preset name "Fast" may hold only lowercase letters, digits and hyphens`},
		}},
		{"name with a space", "a b: {agent: claude, effort: low}", []blueprint.Finding{
			{Line: 3, Path: "presets.a b", Message: `preset name "a b" may hold only lowercase letters, digits and hyphens`},
		}},
		{"no agent", "fast: {effort: low}", []blueprint.Finding{
			{Line: 3, Path: "presets.fast", Message: "a preset needs an agent: one of claude, codex, pi"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := "presets:\n  opus: {agent: claude}\n  " + tt.preset + "\nroles:\n  work: opus\n"

			_, err := repofile.Parse([]byte(data))

			var got []blueprint.Finding
			var verr *blueprint.ValidationError
			if errors.As(err, &verr) {
				got = verr.Findings
			} else if err != nil {
				t.Fatalf("Parse(%q) error = %v, want nil or a *blueprint.ValidationError", data, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, got, tt.want)
			}
		})
	}
}

func TestParseRefusesARoleSmithDoesNotDefine(t *testing.T) {
	t.Parallel()
	data := "presets:\n  opus: {agent: claude}\nroles:\n  work: opus\n  review: opus\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 5, Path: "roles.review", Message: `smith has no role "review": the only role is work`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesARoleNamingAnUndefinedPreset(t *testing.T) {
	t.Parallel()
	data := "presets:\n  opus: {agent: claude}\nroles:\n  work: missing\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "roles.work", Message: `role work names preset "missing", which presets does not define`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesInvalidPresetsAndRolesSetThroughAMerge(t *testing.T) {
	t.Parallel()
	data := "<<:\n  presets:\n    Bad: {agent: cursor, effort: maximum}\n  roles:\n    review: Bad\n    work: missing\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{
		{Line: 3, Path: "presets.Bad", Message: `preset name "Bad" may hold only lowercase letters, digits and hyphens`},
		{Line: 3, Path: "presets.Bad.agent", Message: `unknown agent "cursor": known agents are claude, codex, pi`},
		{Line: 3, Path: "presets.Bad.effort", Message: `unknown effort "maximum": the scale is low, medium, high`},
		{Line: 5, Path: "roles.review", Message: `smith has no role "review": the only role is work`},
		{Line: 6, Path: "roles.work", Message: `role work names preset "missing", which presets does not define`},
	}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesAnInvalidPresetMergedIntoThePresets(t *testing.T) {
	t.Parallel()
	data := "presets:\n  <<: {fast: {agent: cursor}}\n  opus: {agent: claude}\nroles:\n  work: opus\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 2, Path: "presets.fast.agent", Message: `unknown agent "cursor": known agents are claude, codex, pi`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseLoadsPresetsAndRolesSetThroughMerges(t *testing.T) {
	t.Parallel()
	data := "<<:\n  - presets: &shared\n      opus: {agent: claude, model: opus}\n      plain: {agent: cursor}\n    roles: {work: plain}\n" +
		"  - roles: {work: missing}\n" +
		"presets:\n  <<: *shared\n  plain: {agent: codex}\n" +
		"roles:\n  <<: [{work: opus}, {work: missing}]\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	wantPresets := map[string]repofile.Preset{
		"opus":  {Agent: "claude", Model: "opus"},
		"plain": {Agent: "codex"},
	}
	if !reflect.DeepEqual(got.Presets, wantPresets) {
		t.Errorf("Parse(%q) presets = %+v, want %+v", data, got.Presets, wantPresets)
	}
	if want := map[string]string{"work": "opus"}; !reflect.DeepEqual(got.Roles, want) {
		t.Errorf("Parse(%q) roles = %+v, want %+v", data, got.Roles, want)
	}
}
