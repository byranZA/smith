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
	data := "agent: claude\neffort: high\nmodle: opus\n"

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
	data := "agent: codex\nmodel: gpt-5\neffort: low\npush: false\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	off := false
	want := repofile.File{Agent: "codex", Model: "gpt-5", Effort: "low", Push: &off}
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
	data := "agent: claude\nlabels:\n  agent: go\n  task: ready\n"

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
	data := "model: &empty \"\"\nlabels:\n  agent: *empty\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Path: "labels.agent", Message: "a label needs a name: set one, or leave the key out for the default"}}
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
	data := "model: &name go\nlabels:\n  agent: *name\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Model: "go", Labels: repofile.Labels{Agent: "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}
