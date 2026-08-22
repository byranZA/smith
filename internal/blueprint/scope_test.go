package blueprint_test

import (
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParseRefusesTheSpikesMismatchedScopes(t *testing.T) {
	data, err := os.ReadFile("testdata/scope.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	if _, parseErr := blueprint.Parse(data); parseErr == nil {
		t.Fatal("Parse(scope.yaml) err = nil, want both mismatched destinations refused")
	}
	got := findings(t, string(data))

	if len(got) != 2 {
		t.Fatalf("Findings = %+v, want one per mismatched placement", got)
	}
	want := []string{"placements[0].to", "repos[0].placements[0].to"}
	for i, path := range want {
		if got[i].Path != path {
			t.Errorf("Findings[%d].Path = %q, want %q", i, got[i].Path, path)
		}
	}
}

func TestParsePlacementDestinationMustMatchItsScope(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		valid bool
	}{
		{"box absolute", "placements:\n  - from: file:~/a\n    to: /etc/gitconfig\n", true},
		{"box home relative", "placements:\n  - from: file:~/a\n    to: ~/.gitconfig\n", true},
		{"box relative", "placements:\n  - from: file:~/a\n    to: etc/gitconfig\n", false},
		{"repo worktree relative", "repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: file:~/a\n        to: .env.local\n", true},
		{"repo absolute", "repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: file:~/a\n        to: /etc/npmrc\n", false},
		{"repo home relative", "repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: file:~/a\n        to: ~/.npmrc\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := blueprint.Parse([]byte(tt.doc))
			if tt.valid && err != nil {
				t.Fatalf("Parse() err = %v, want the destination accepted in its own scope", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("Parse() err = nil, want the destination refused as out of scope")
			}
		})
	}
}

func TestParseNamesTheFixForAMismatchedDestination(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
		want []string
	}{
		{
			name: "box scope names the required shape",
			doc:  "placements:\n  - from: file:~/a\n    to: etc/gitconfig\n",
			path: "placements[0].to",
			want: []string{`"etc/gitconfig"`, "absolute", "~"},
		},
		{
			name: "repo scope names box scope as the alternative",
			doc:  "repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: file:~/a\n        to: ~/.npmrc\n",
			path: "repos[0].placements[0].to",
			want: []string{`"~/.npmrc"`, "worktree-relative", "box scope"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.doc)
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", got)
			}
			if got[0].Path != tt.path {
				t.Errorf("Path = %q, want %q so the report names the placement by scope and index", got[0].Path, tt.path)
			}
			for _, want := range tt.want {
				if !strings.Contains(got[0].Message, want) {
					t.Errorf("Message = %q, want it to mention %s", got[0].Message, want)
				}
			}
		})
	}
}
