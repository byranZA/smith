package repofile_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/repofile"
)

func TestParseRefusesAnUnknownKeyNamingItsLine(t *testing.T) {
	t.Parallel()
	data := "push: true\nprepare: make\nmodle: opus\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Message: `unknown field "modle"`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseReadsEveryField(t *testing.T) {
	t.Parallel()
	data := "presets:\n  codex: {agent: codex, model: gpt-5, effort: low}\nroles:\n  work: codex\npush: false\nprepare: make\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	off := false
	want := repofile.File{
		Presets: map[string]repofile.Preset{"codex": {Agent: "codex", Model: "gpt-5", Effort: "low"}},
		Roles:   map[string]string{"work": "codex"},
		Push:    &off,
		Prepare: "make",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseReadsTheLabelsBlock(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: go\n  human: stop\n  spec: epic\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Labels: repofile.Labels{Agent: "go", Human: "stop", Spec: "epic"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAnUnknownLabelKeyNamingItsLine(t *testing.T) {
	t.Parallel()
	data := "push: true\nlabels:\n  agent: go\n  task: ready\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels", Message: `unknown field "task"`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesAnEmptyLabelNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"quoted empty": "labels:\n  spec: epic\n  agent: \"\"\n",
		"no value":     "labels:\n  spec: epic\n  agent:\n",
		"null":         "labels:\n  spec: epic\n  agent: ~\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 3, Path: "labels.agent", Message: "a label needs a name: set one, or leave the key out for the default"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseRefusesTwoLabelsSharingANameNamingTheLaterLine(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: triaged\n  spec: epic\n  human: triaged\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels.human", Message: `label "triaged" is already labels.agent, on line 2: each label needs a name of its own`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesAnEmptyLabelGivenThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "presets:\n  plain: {agent: claude, model: &empty \"\"}\nlabels:\n  agent: *empty\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels.agent", Message: "a label needs a name: set one, or leave the key out for the default"}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesTwoLabelsSharingANameThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: &shared triaged\n  human: *shared\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Path: "labels.human", Message: `label "triaged" is already labels.agent, on line 2: each label needs a name of its own`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseReadsALabelGivenThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "prepare: &name go\nlabels:\n  agent: *name\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Prepare: "go", Labels: repofile.Labels{Agent: "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseReadsThePrepareCommand(t *testing.T) {
	t.Parallel()
	data := "push: true\nprepare: uv sync\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	on := true
	want := repofile.File{Push: &on, Prepare: "uv sync"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAMalformedPrepareCommandNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"list":         "push: true\nprepare:\n  - uv sync\n",
		"mapping":      "push: true\nprepare:\n  run: uv sync\n",
		"quoted empty": "push: true\nprepare: \"\"\n",
		"no value":     "push: true\nprepare:\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 2, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseReadsAPrepareCommandSuppliedThroughAMerge(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"mapping":           "push: true\n<<: {prepare: uv sync}\n",
		"sequence":          "push: true\n<<: [{push: true}, {prepare: uv sync}]\n",
		"nested":            "push: true\n<<: {<<: {prepare: uv sync}}\n",
		"direct overrides":  "push: true\nprepare: uv sync\n<<: {prepare: []}\n",
		"first merge wins":  "push: true\n<<: [{prepare: uv sync}, {prepare: {}}]\n",
		"own key overrides": "push: true\n<<: {prepare: uv sync, <<: {prepare: \"\"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := repofile.Parse([]byte(data))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", data, err)
			}

			if got.Prepare != "uv sync" {
				t.Errorf("Parse(%q).Prepare = %q, want %q", data, got.Prepare, "uv sync")
			}
		})
	}
}

func TestParseRefusesAMalformedMergedPrepareCommandNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"list":             "push: true\n<<: {prepare: []}\n",
		"mapping":          "push: true\n<<: {prepare: {}}\n",
		"quoted empty":     "push: true\n<<: {prepare: \"\"}\n",
		"sequence":         "push: true\n<<: [{push: true}, {prepare: []}]\n",
		"nested":           "push: true\n<<: {<<: {prepare: []}}\n",
		"first merge wins": "push: true\n<<: [{prepare: []}, {prepare: uv sync}]\n",
		"direct malformed": "push: true\nprepare: []\n<<: {prepare: uv sync}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 2, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseRefusesAMalformedPrepareCommandOnTheLineOfItsMergedKey(t *testing.T) {
	t.Parallel()
	data := "push: true\n<<:\n  prepare: []\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}
