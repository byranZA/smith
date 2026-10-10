package repofile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
)

func locate(t *testing.T, root string) repofile.Repo {
	t.Helper()
	home := t.TempDir()
	repo, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home))
	if err != nil {
		t.Fatalf("Locate(%q, home %q) error = %v", root, home, err)
	}
	return repo
}

func scaffold(t *testing.T, repo repofile.Repo, values repofile.File) repofile.Outcome {
	t.Helper()
	outcome, err := repofile.Scaffold(repo, values)
	if err != nil {
		t.Fatalf("Scaffold(%q, %+v) error = %v", repo.Path(), values, err)
	}
	return outcome
}

func readRepoFile(t *testing.T, repo repofile.Repo) repofile.File {
	t.Helper()
	data, err := os.ReadFile(repo.Path())
	if err != nil {
		t.Fatal(err)
	}
	f, err := repofile.Parse(data)
	if err != nil {
		t.Fatalf("Parse(written repo file) error = %v", err)
	}
	return f
}

func TestScaffoldWritesTheStarterAtTheRepoRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := locate(t, root)

	outcome := scaffold(t, repo, repofile.File{})

	want := repofile.Outcome{Path: filepath.Join(root, ".smith", "repo.yaml"), Created: true}
	if outcome != want {
		t.Errorf("Scaffold(%q, %+v) = %+v, want %+v", repo.Path(), repofile.File{}, outcome, want)
	}
}

func TestTheUneditedStarterSetsNothing(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.File{})

	if got := readRepoFile(t, repo); got != (repofile.File{}) {
		t.Errorf("starter = %+v, want every field unset", got)
	}
}

func TestTheStarterDocumentsEverySettingWithAnExample(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.File{})
	data, err := os.ReadFile(repo.Path())
	if err != nil {
		t.Fatal(err)
	}
	example := regexp.MustCompile(`(?m)^# ([a-z]+: )`).ReplaceAll(data, []byte("$1"))

	got, err := repofile.Parse(example)

	off := false
	want := repofile.File{Agent: "claude", Model: "opus", Effort: "high", Push: &off}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(starter with its examples uncommented) = %+v, %v; want %+v", got, err, want)
	}
}

func TestScaffoldFillsInTheValuesGiven(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.File{Agent: "claude", Model: "opus", Effort: "high"})

	want := repofile.File{Agent: "claude", Model: "opus", Effort: "high"}
	if got := readRepoFile(t, repo); got != want {
		t.Errorf("repo file = %+v, want %+v", got, want)
	}
}

func TestScaffoldFillsInOnlyTheValuesGiven(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.File{Model: "anthropic/some: model"})

	want := repofile.File{Model: "anthropic/some: model"}
	if got := readRepoFile(t, repo); got != want {
		t.Errorf("repo file = %+v, want %+v", got, want)
	}
}

func TestScaffoldLeavesAnExistingRepoFileAlone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := locate(t, root)
	existing := "agent: codex\n"
	if err := os.MkdirAll(filepath.Dir(repo.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.Path(), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := scaffold(t, repo, repofile.File{Agent: "claude"})

	if want := (repofile.Outcome{Path: repo.Path(), Created: false}); outcome != want {
		t.Errorf("Scaffold(%q, %+v) = %+v, want %+v", repo.Path(), repofile.File{Agent: "claude"}, outcome, want)
	}
	if data, err := os.ReadFile(repo.Path()); err != nil || string(data) != existing {
		t.Errorf("repo file = %q (%v), want it unchanged as %q", data, err, existing)
	}
}

func TestScaffoldWritesNoPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scaffold(t, locate(t, root), repofile.File{})

	entries, err := os.ReadDir(filepath.Join(root, ".smith"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "repo.yaml" {
		t.Errorf(".smith holds %v, want only repo.yaml", entries)
	}
}
