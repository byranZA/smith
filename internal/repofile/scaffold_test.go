package repofile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
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

func scaffold(t *testing.T, repo repofile.Repo, values repofile.Preset) repofile.Outcome {
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

	outcome := scaffold(t, repo, repofile.Preset{})

	want := repofile.Outcome{Path: filepath.Join(root, ".smith", "repo.yaml"), Created: true}
	if outcome != want {
		t.Errorf("Scaffold(%q, %+v) = %+v, want %+v", repo.Path(), repofile.Preset{}, outcome, want)
	}
}

func TestTheUneditedStarterRunsTheWorkRoleOnClaudeAlone(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.Preset{})

	want := repofile.File{Presets: map[string]repofile.Preset{"claude": {Agent: "claude"}}, Roles: map[string]string{"work": "claude"}}
	if got := readRepoFile(t, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("starter = %+v, want %+v", got, want)
	}
}

func TestTheStarterDocumentsEverySettingWithAnExample(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.Preset{})
	data, err := os.ReadFile(repo.Path())
	if err != nil {
		t.Fatal(err)
	}
	example := regexp.MustCompile(`(?m)^( *)# ((?:  )*[a-z]+:(?: |$))`).ReplaceAll(data, []byte("$1$2"))

	got, err := repofile.Parse(example)

	off := false
	want := repofile.File{
		Presets: map[string]repofile.Preset{"claude": {Agent: "claude", Model: "opus", Effort: "high"}},
		Roles:   map[string]string{"work": "claude"},
		Push:    &off, Prepare: "uv sync",
		Labels: repofile.Labels{Agent: "ready-for-agent", Human: "ready-for-human", Spec: "spec"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(starter with its examples uncommented) = %+v, %v; want %+v", got, err, want)
	}
}

func TestScaffoldFillsInThePresetGiven(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.Preset{Agent: "codex", Model: "gpt-6.1-sol", Effort: "high"})

	want := repofile.File{
		Presets: map[string]repofile.Preset{"codex": {Agent: "codex", Model: "gpt-6.1-sol", Effort: "high"}},
		Roles:   map[string]string{"work": "codex"},
	}
	if got := readRepoFile(t, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("repo file = %+v, want %+v", got, want)
	}
}

func TestScaffoldFillsInOnlyTheValuesGiven(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	scaffold(t, repo, repofile.Preset{Model: "anthropic/some: model"})

	want := repofile.File{
		Presets: map[string]repofile.Preset{"claude": {Agent: "claude", Model: "anthropic/some: model"}},
		Roles:   map[string]string{"work": "claude"},
	}
	if got := readRepoFile(t, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("repo file = %+v, want %+v", got, want)
	}
}

func TestScaffoldRefusesAPresetThatWouldNotLoad(t *testing.T) {
	t.Parallel()
	repo := locate(t, t.TempDir())
	values := repofile.Preset{Agent: "cursor"}

	_, err := repofile.Scaffold(repo, values)

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Scaffold(%q, %+v) error = %v, want a *blueprint.ValidationError", repo.Path(), values, err)
	}
	if _, statErr := os.Stat(repo.Path()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("Scaffold(%q, %+v) left %q (%v), want no repo file written", repo.Path(), values, repo.Path(), statErr)
	}
}

func TestScaffoldLeavesAnExistingRepoFileAlone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := locate(t, root)
	existing := "push: false\n"
	if err := os.MkdirAll(filepath.Dir(repo.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.Path(), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := scaffold(t, repo, repofile.Preset{Agent: "claude"})

	if want := (repofile.Outcome{Path: repo.Path(), Created: false}); outcome != want {
		t.Errorf("Scaffold(%q, %+v) = %+v, want %+v", repo.Path(), repofile.Preset{Agent: "claude"}, outcome, want)
	}
	if data, err := os.ReadFile(repo.Path()); err != nil || string(data) != existing {
		t.Errorf("repo file = %q (%v), want it unchanged as %q", data, err, existing)
	}
}

func TestScaffoldWritesNoPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scaffold(t, locate(t, root), repofile.Preset{})

	entries, err := os.ReadDir(filepath.Join(root, ".smith"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "repo.yaml" {
		t.Errorf(".smith holds %v, want only repo.yaml", entries)
	}
}
