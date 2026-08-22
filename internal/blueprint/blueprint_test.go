package blueprint_test

import (
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
