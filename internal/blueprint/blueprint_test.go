package blueprint_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

const validDoc = `
access:    tailscale
terminal:  tmux
workspace: ~/workspace

repos:
  - name: acme-api
    url:  git@github.com:acme/api.git
    base: develop
`

func TestParseValidBlueprint(t *testing.T) {
	got, err := blueprint.Parse([]byte(validDoc))
	if err != nil {
		t.Fatalf("Parse() err = %v, want nil", err)
	}
	if got.Access != "tailscale" {
		t.Errorf("Access = %q, want %q", got.Access, "tailscale")
	}
	if got.Terminal != "tmux" {
		t.Errorf("Terminal = %q, want %q", got.Terminal, "tmux")
	}
	if got.Workspace != "~/workspace" {
		t.Errorf("Workspace = %q, want %q", got.Workspace, "~/workspace")
	}
	if len(got.Repos) != 1 {
		t.Fatalf("len(Repos) = %d, want 1", len(got.Repos))
	}
	repo := got.Repos[0]
	if repo.Name != "acme-api" || repo.URL != "git@github.com:acme/api.git" || repo.Base != "develop" {
		t.Errorf("Repos[0] = %+v, want name acme-api, url git@github.com:acme/api.git, base develop", repo)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"unknown top-level key", "access: public\nterminals: tmux\n"},
		{"unknown key inside a repo", "repos:\n  - url: git@github.com:acme/api.git\n    ref: main\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := blueprint.Parse([]byte(tt.doc)); err == nil {
				t.Fatalf("Parse(%q) err = nil, want an error refusing the unknown field", tt.doc)
			}
		})
	}
}

func TestParseRejectsMalformedDocument(t *testing.T) {
	if _, err := blueprint.Parse([]byte("access: [unclosed\n")); err == nil {
		t.Fatal("Parse() err = nil, want an error for an unparseable document")
	}
}

func TestParseEmptyDocumentIsValid(t *testing.T) {
	got, err := blueprint.Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) err = %v, want nil", err)
	}
	if got.Access != "" || len(got.Repos) != 0 {
		t.Errorf("Parse(nil) = %+v, want the zero blueprint", got)
	}
}

func TestParseReportsEveryFindingInOneRun(t *testing.T) {
	const doc = `access: public
terminals: tmux
workspace: ~/workspace
sessions: 3
`

	_, err := blueprint.Parse([]byte(doc))

	var invalid *blueprint.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Parse() err = %v (%T), want a *blueprint.ValidationError", err, err)
	}
	if invalid.Malformed {
		t.Error("Malformed = true, want false for a document that parses but declares unknown fields")
	}
	want := []blueprint.Finding{
		{Line: 2, Message: `unknown field "terminals"`},
		{Line: 4, Message: `unknown field "sessions"`},
	}
	if len(invalid.Findings) != len(want) {
		t.Fatalf("Findings = %+v, want %d findings", invalid.Findings, len(want))
	}
	for i, w := range want {
		if invalid.Findings[i] != w {
			t.Errorf("Findings[%d] = %+v, want %+v", i, invalid.Findings[i], w)
		}
	}
}

func TestParseNamesTheRepoAnUnknownFieldAppearsIn(t *testing.T) {
	const doc = `repos:
  - url: git@github.com:acme/api.git
  - url: git@github.com:acme/web.git
    ref: main
`

	_, err := blueprint.Parse([]byte(doc))

	var invalid *blueprint.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Parse() err = %v (%T), want a *blueprint.ValidationError", err, err)
	}
	if len(invalid.Findings) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", invalid.Findings)
	}
	got := invalid.Findings[0]
	if got.Line != 4 {
		t.Errorf("Line = %d, want 4", got.Line)
	}
	if got.Path != "repos[1]" {
		t.Errorf("Path = %q, want %q so the report names which repo", got.Path, "repos[1]")
	}
	if !strings.Contains(invalid.Error(), "repos[1]") {
		t.Errorf("Error() = %q, want it to name the second repo", invalid.Error())
	}
}

func TestParseReportSpeaksOnlyTheOperatorsVocabulary(t *testing.T) {
	docs := map[string]string{
		"unknown top-level field": "terminals: tmux\n",
		"unknown field in a repo": "repos:\n  - url: git@github.com:acme/api.git\n    ref: main\n",
		"wrong shape for a field": "repos: 3\n",
		"wrong shape for a value": "access:\n  - public\n",
		"malformed document":      "access: [unclosed\n",
	}
	leaks := []string{"blueprint.", "main.", "yaml", "[]", "type ", "unmarshal", "!!"}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			_, err := blueprint.Parse([]byte(doc))
			if err == nil {
				t.Fatalf("Parse(%q) err = nil, want a report", doc)
			}
			report := err.Error()
			for _, leak := range leaks {
				if strings.Contains(report, leak) {
					t.Errorf("Error() = %q, want no %q in an operator-facing report", report, leak)
				}
			}
		})
	}
}

func TestParseReportsMalformedDocumentWithItsPosition(t *testing.T) {
	_, err := blueprint.Parse([]byte("access: public\n\tterminal: tmux\n"))

	var invalid *blueprint.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Parse() err = %v (%T), want a *blueprint.ValidationError", err, err)
	}
	if !invalid.Malformed {
		t.Error("Malformed = false, want true for a document that is not YAML at all")
	}
	if len(invalid.Findings) != 1 || invalid.Findings[0].Line != 2 {
		t.Fatalf("Findings = %+v, want one finding positioned on line 2", invalid.Findings)
	}
	report := invalid.Error()
	if !strings.Contains(report, "malformed") {
		t.Errorf("Error() = %q, want it to word the failure as malformed", report)
	}
	if strings.Contains(report, "unknown field") {
		t.Errorf("Error() = %q, want wording distinct from an unknown-field report", report)
	}
}
