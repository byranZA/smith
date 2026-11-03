package repofile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

func scaffold(t *testing.T, repo repofile.Repo, values repofile.Preset) []repofile.Outcome {
	t.Helper()
	outcomes, err := repofile.Scaffold(repo, values)
	if err != nil {
		t.Fatalf("Scaffold(%q, %+v) error = %v", repo.Path(), values, err)
	}
	return outcomes
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

	outcomes := scaffold(t, repo, repofile.Preset{})

	want := []repofile.Outcome{
		{Path: filepath.Join(root, ".smith", "repo.yaml"), Created: true},
		{Path: filepath.Join(root, ".smith", "README.md"), Created: true},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("Scaffold(%q, %+v) = %+v, want %+v", repo.Path(), repofile.Preset{}, outcomes, want)
	}
}

func TestScaffoldWritesOnlyTheRepoGuideBesideAnExistingRepoFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := locate(t, root)
	writeSmithFile(t, repo.Path(), "push: false\n")

	outcomes := scaffold(t, repo, repofile.Preset{})

	want := []repofile.Outcome{
		{Path: filepath.Join(root, ".smith", "repo.yaml"), Created: false},
		{Path: filepath.Join(root, ".smith", "README.md"), Created: true},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("Scaffold() = %+v, want %+v", outcomes, want)
	}
	if data, err := os.ReadFile(repo.Path()); err != nil || string(data) != "push: false\n" {
		t.Errorf("repo file = %q (%v), want it unchanged", data, err)
	}
}

func TestScaffoldNeverOverwritesAnExistingRepoGuide(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := locate(t, root)
	guide := filepath.Join(root, ".smith", "README.md")
	writeSmithFile(t, guide, "my own notes\n")

	outcomes := scaffold(t, repo, repofile.Preset{})

	if got := outcomes[1]; got.Path != guide || got.Created {
		t.Errorf("guide outcome = %+v, want %s left alone", got, guide)
	}
	if data, err := os.ReadFile(guide); err != nil || string(data) != "my own notes\n" {
		t.Errorf("repo guide = %q (%v), want it unchanged", data, err)
	}
}

func TestTheRepoGuidePointsAtTheAgentGuideAndTheWorkBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scaffold(t, locate(t, root), repofile.Preset{})

	data, err := os.ReadFile(filepath.Join(root, ".smith", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"smith help agents", "smith <verb> --help", "smith loop list <n>", ".smith/repo.yaml", ".smith/prompt.md", "does not run smith"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("repo guide lacks %q:\n%s", want, data)
		}
	}
}

func writeSmithFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
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
		Roles:   map[string]string{"work": "claude", "review": "claude"},
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

	outcomes := scaffold(t, repo, repofile.Preset{Agent: "claude"})

	if want := (repofile.Outcome{Path: repo.Path(), Created: false}); outcomes[0] != want {
		t.Errorf("Scaffold(%q, %+v)[0] = %+v, want %+v", repo.Path(), repofile.Preset{Agent: "claude"}, outcomes[0], want)
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
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"README.md", "repo.yaml"}; !reflect.DeepEqual(names, want) {
		t.Errorf(".smith holds %v, want %v", names, want)
	}
}
