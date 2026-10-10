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
