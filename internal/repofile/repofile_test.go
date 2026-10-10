package repofile_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/repofile"
)

func TestParseRefusesAnUnknownKeyNamingItsLine(t *testing.T) {
	_, err := repofile.Parse([]byte("agent: claude\neffort: high\nmodle: opus\n"))

	if err == nil || !strings.Contains(err.Error(), `line 3: unknown field "modle"`) {
		t.Errorf("Parse() error = %v, want it to name \"modle\" on line 3", err)
	}
}

func TestParseReadsEveryField(t *testing.T) {
	got, err := repofile.Parse([]byte("agent: codex\nmodel: gpt-5\neffort: low\npush: false\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	off := false
	want := repofile.File{Agent: "codex", Model: "gpt-5", Effort: "low", Push: &off}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}
