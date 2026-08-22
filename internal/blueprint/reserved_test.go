package blueprint_test

import (
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParseRefusesTheSpikesMisindentedBase(t *testing.T) {
	data, err := os.ReadFile("testdata/footgun.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	_, parseErr := blueprint.Parse(data)
	if parseErr == nil {
		t.Fatal("Parse(footgun.yaml) err = nil, want the misindented base refused")
	}
	got := findings(t, string(data))

	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Line != 7 {
		t.Errorf("Line = %d, want 7, the line base was misindented onto", got[0].Line)
	}
	if got[0].Path != "repos[0].tools" {
		t.Errorf("Path = %q, want %q so the report names the map it landed in", got[0].Path, "repos[0].tools")
	}
	for _, want := range []string{`"base"`, "indentation"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("Message = %q, want it to mention %s", got[0].Message, want)
		}
	}
}

func TestParseReservesSchemaFieldNamesInBothOpenMapsAtBothScopes(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
	}{
		{"box tools", "tools:\n  provider: \"22\"\n", "tools"},
		{"box env", "env:\n  url: literal:x\n", "env"},
		{"repo tools", "repos:\n  - url: git@github.com:acme/api.git\n    tools:\n      base: develop\n", "repos[0].tools"},
		{"repo env", "repos:\n  - url: git@github.com:acme/api.git\n    env:\n      mode: literal:x\n", "repos[0].env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.doc)
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", got)
			}
			if got[0].Path != tt.path {
				t.Errorf("Path = %q, want %q", got[0].Path, tt.path)
			}
			if !strings.Contains(got[0].Message, "indentation") {
				t.Errorf("Message = %q, want it to point at the indentation", got[0].Message)
			}
		})
	}
}

func TestParseReservesEverySchemaFieldName(t *testing.T) {
	reserved := []string{
		"access", "terminal", "workspace", "provider", "packages", "tools",
		"env", "placements", "repos", "git", "name", "url", "base", "from",
		"to", "mode", "perms",
	}
	for _, field := range reserved {
		t.Run(field, func(t *testing.T) {
			got := findings(t, "env:\n  "+field+": literal:x\n")
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want %q refused under env", got, field)
			}
			if !strings.Contains(got[0].Message, `"`+field+`"`) {
				t.Errorf("Message = %q, want it to name %q", got[0].Message, field)
			}
		})
	}
}

func TestParseReservationIsCaseSensitive(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"url", false},
		{"URL", true},
		{"mode", false},
		{"MODE", true},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, err := blueprint.Parse([]byte("env:\n  " + tt.key + ": literal:x\n"))
			if tt.valid && err != nil {
				t.Fatalf("Parse(env %s) err = %v, want a real environment variable accepted", tt.key, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("Parse(env %s) err = nil, want the schema field name refused", tt.key)
			}
		})
	}
}
