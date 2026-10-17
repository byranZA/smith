package session_test

import (
	"testing"

	"github.com/byranZA/smith/internal/session"
)

func TestCloneIsTheDeclaredReposBareClone(t *testing.T) {
	env := session.Env{Workspace: "/home/dev/workspace", Repos: []session.Repo{{Name: "smith"}}}

	got, err := env.Clone("smith")
	if err != nil {
		t.Fatalf("Clone(%q) err = %v", "smith", err)
	}

	if want := "/home/dev/workspace/smith/repo.git"; got != want {
		t.Errorf("Clone(%q) = %q, want %q", "smith", got, want)
	}
}

func TestCloneRefusesAnUndeclaredRepo(t *testing.T) {
	env := session.Env{Workspace: "/home/dev/workspace", Repos: []session.Repo{{Name: "smith"}}}

	_, err := env.Clone("nope")

	want := `repo "nope" is not declared in the blueprint this box was built from; it declares smith`
	if err == nil || err.Error() != want {
		t.Errorf("Clone(%q) err = %v, want %q", "nope", err, want)
	}
}
