package repofile_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/repofile"
)

func TestLoadWithNoRepoFileSetsNothing(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())

	got, err := repofile.Load(repo)

	if err != nil || !reflect.DeepEqual(got, repofile.File{}) {
		t.Errorf("Load(%q) = %+v, %v; want an empty File and no error", repo.Path(), got, err)
	}
}

func TestLoadReadsTheRepoFile(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	writeRepoFile(t, repo, "presets:\n  codex: {agent: codex, model: opus}\n")

	got, err := repofile.Load(repo)

	want := repofile.File{Presets: map[string]repofile.Preset{"codex": {Agent: "codex", Model: "opus"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load(%q) = %+v, %v; want %+v", repo.Path(), got, err, want)
	}
}

func TestLoadRefusesAnUnknownKeyNamingItsLineAndTheFile(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	writeRepoFile(t, repo, "push: true\n\nmodle: opus\n")

	_, err := repofile.Load(repo)

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Load(%q) error = %v, want a *blueprint.ValidationError", repo.Path(), err)
	}
	want := []blueprint.Finding{{Line: 3, Message: `unknown field "modle"`}}
	if !reflect.DeepEqual(verr.Findings, want) || !strings.Contains(err.Error(), repo.Path()) {
		t.Errorf("Load(%q) error = %v, findings %+v; want it to name the file and findings %+v", repo.Path(), err, verr.Findings, want)
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

func TestLoadAtAWorktreeReadsItsOwnRepoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte("prepare: uv sync\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := repofile.Load(repofile.At(dir))

	want := repofile.File{Prepare: "uv sync"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load(At(%q)) = %+v, %v; want %+v", dir, got, err, want)
	}
}
