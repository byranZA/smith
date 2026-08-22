package blueprint_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParseAcceptsGitIdentity(t *testing.T) {
	doc := "git:\n  user_name: Ada Lovelace\n  user_email: ada@acme.example\n"

	got, err := blueprint.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse() err = %v, want git identity accepted on its own", err)
	}
	if got.Git.UserName != "Ada Lovelace" || got.Git.UserEmail != "ada@acme.example" {
		t.Errorf("Git = %+v, want the declared identity", got.Git)
	}
}

func TestParseAcceptsAGitconfigPlacementWithoutIdentity(t *testing.T) {
	doc := "placements:\n  - from: file:~/.secrets/gitconfig\n    to: ~/.gitconfig\n"

	if _, err := blueprint.Parse([]byte(doc)); err != nil {
		t.Fatalf("Parse() err = %v, want a gitconfig placement accepted on its own", err)
	}
}

func TestParseRefusesGitIdentityBesideAGitconfigPlacement(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{
			name: "name only",
			doc:  "git:\n  user_name: Ada Lovelace\nplacements:\n  - from: file:~/.secrets/gitconfig\n    to: ~/.gitconfig\n",
		},
		{
			name: "email only",
			doc:  "git:\n  user_email: ada@acme.example\nplacements:\n  - from: file:~/.secrets/gitconfig\n    to: ~/.gitconfig\n",
		},
		{
			name: "both, beside other placements",
			doc: "git:\n  user_name: Ada Lovelace\n  user_email: ada@acme.example\n" +
				"placements:\n  - from: file:~/.secrets/id\n    to: ~/.ssh/id_forge\n" +
				"  - from: file:~/.secrets/gitconfig\n    to: ~/.gitconfig\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.doc)
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", got)
			}
			if got[0].Path != "git" {
				t.Errorf("Path = %q, want the finding reported against the identity fields", got[0].Path)
			}
			for _, want := range []string{"~/.gitconfig", "mutually exclusive", "choose"} {
				if !strings.Contains(got[0].Message, want) {
					t.Errorf("Message = %q, want it to mention %q", got[0].Message, want)
				}
			}
		})
	}
}

func TestParseKeepsAGitconfigPlacementInsideARepoOutOfTheExclusion(t *testing.T) {
	doc := "git:\n  user_name: Ada Lovelace\n" +
		"repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: file:~/.secrets/gitconfig\n        to: .gitconfig\n"

	if _, err := blueprint.Parse([]byte(doc)); err != nil {
		t.Fatalf("Parse() err = %v, want a worktree .gitconfig accepted beside git identity", err)
	}
}
