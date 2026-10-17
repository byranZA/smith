package repofile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/repofile"
)

// bareWith clones into a bare repo a one-commit repo on main holding files, keyed by path.
func bareWith(t *testing.T, files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	runGit(t, src, "init", "-b", "main")
	files["README.md"] = "base\n"
	for path, body := range files {
		full := filepath.Join(src, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("make %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "-c", "user.email=smith@example.com", "-c", "user.name=smith", "commit", "-m", "base")
	bare := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, src, "clone", "--bare", src, bare)
	return bare
}

// runGit runs a real git command in dir and fails the test if it does not.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	var errOut strings.Builder
	if err := connection.System().Run(t.Context(), "git", append([]string{"-C", dir}, args...), nil, &strings.Builder{}, &errOut); err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
}

func TestCommittedReadsTheRepoFileAndPromptAtARef(t *testing.T) {
	t.Parallel()
	bare := bareWith(t, map[string]string{".smith/repo.yaml": "agent: codex\n", ".smith/prompt.md": "Do #{{TASK_NUMBER}}.\n"})

	got, err := repofile.Committed(t.Context(), connection.System(), bare, "main")
	if err != nil {
		t.Fatalf("Committed(%q, main) err = %v", bare, err)
	}

	want := repofile.Commit{File: repofile.File{Agent: "codex"}, Prompt: []byte("Do #{{TASK_NUMBER}}.\n"), Ejected: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Committed(%q, main) = %+v, want %+v", bare, got, want)
	}
}

func TestCommittedWithNeitherFileSetsNothing(t *testing.T) {
	t.Parallel()
	bare := bareWith(t, map[string]string{})

	got, err := repofile.Committed(t.Context(), connection.System(), bare, "HEAD")
	if err != nil {
		t.Fatalf("Committed(%q, HEAD) err = %v", bare, err)
	}

	if !reflect.DeepEqual(got, repofile.Commit{}) {
		t.Errorf("Committed(%q, HEAD) = %+v, want the zero Commit", bare, got)
	}
}

func TestCommittedRefusesARefTheRepoLacks(t *testing.T) {
	t.Parallel()
	bare := bareWith(t, map[string]string{})

	_, err := repofile.Committed(t.Context(), connection.System(), bare, "nope")

	if err == nil || !strings.Contains(err.Error(), "read the repo file at nope in "+bare) {
		t.Errorf("Committed(%q, nope) err = %v, want it to name the ref and the repo", bare, err)
	}
}

func TestCommittedRefusesAnUnknownKeyNamingTheRef(t *testing.T) {
	t.Parallel()
	bare := bareWith(t, map[string]string{".smith/repo.yaml": "agnet: codex\n"})

	_, err := repofile.Committed(t.Context(), connection.System(), bare, "main")

	if err == nil || !strings.Contains(err.Error(), "main:.smith/repo.yaml") {
		t.Errorf("Committed(%q, main) err = %v, want it to name the file at the ref", bare, err)
	}
}
