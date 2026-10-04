package repofile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/repofile"
)

func TestLoadWithNoRepoFileSetsNothing(t *testing.T) {
	repo := locate(t, t.TempDir())

	got, err := repofile.Load(repo)

	if err != nil || got != (repofile.File{}) {
		t.Errorf("Load() = %+v, %v; want an empty File and no error", got, err)
	}
}

func TestLoadReadsTheRepoFile(t *testing.T) {
	repo := locate(t, t.TempDir())
	writeRepoFile(t, repo, "agent: codex\nmodel: opus\n")

	got, err := repofile.Load(repo)

	want := repofile.File{Agent: "codex", Model: "opus"}
	if err != nil || got != want {
		t.Errorf("Load() = %+v, %v; want %+v", got, err, want)
	}
}

func TestLoadRefusesAnUnknownKeyNamingItsLineAndTheFile(t *testing.T) {
	repo := locate(t, t.TempDir())
	writeRepoFile(t, repo, "agent: claude\n\nmodle: opus\n")

	_, err := repofile.Load(repo)

	if err == nil || !strings.Contains(err.Error(), `line 3: unknown field "modle"`) || !strings.Contains(err.Error(), repo.Path()) {
		t.Errorf("Load() error = %v, want it to name %s and \"modle\" on line 3", err, repo.Path())
	}
}

func writeRepoFile(t *testing.T, repo repofile.Repo, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(repo.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.Path(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
