package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// toplevelGit answers `git rev-parse --show-toplevel` with root, failing when root is empty.
type toplevelGit struct {
	root string
}

func (g toplevelGit) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if g.root == "" {
		_, _ = io.WriteString(stderr, "fatal: not a git repository")
		return exitStatus(128)
	}
	if len(args) != 4 || args[2] != "rev-parse" {
		_, _ = io.WriteString(stderr, "error: No such remote 'origin'")
		return exitStatus(2)
	}
	_, err := fmt.Fprintln(stdout, g.root)
	return err
}

// runRepoInit runs `smith repo init` from workdir in a repo rooted at root.
func runRepoInit(t *testing.T, root, workdir, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runRepoInitAt(t, nil, root, workdir, home, args...)
}

// runRepoInitAt is runRepoInit with term as the terminal; nil is nobody there.
func runRepoInitAt(t *testing.T, term terminal, root, workdir, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newRepoCmd(func() (config.Home, error) { return config.NewHome(home), nil }, toplevelGit{root: root}, func() (string, error) { return workdir, nil }, term)
	cmd.SetArgs(append([]string{"init"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestRepoInitReportsTheRepoFileCreatedAtTheRepoRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "see .smith/README.md\n")

	stdout, stderr, code := runRepoInit(t, root, filepath.Join(root, "internal"), t.TempDir())

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := "created " + filepath.Join(root, ".smith", "repo.yaml") + "\ncreated " + filepath.Join(root, ".smith", "README.md") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRepoInitReportsAnExistingRepoFileAsLeftAlone(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".smith", "repo.yaml")
	writeFile(t, filepath.Join(root, "AGENTS.md"), "see .smith/README.md\n")
	writeFile(t, path, "push: false\n")

	stdout, _, _ := runRepoInit(t, root, root, t.TempDir())

	want := "left alone " + path + "\ncreated " + filepath.Join(root, ".smith", "README.md") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRepoInitFillsInTheFlags(t *testing.T) {
	root := t.TempDir()

	runRepoInit(t, root, root, t.TempDir(), "--agent", "codex", "--model", "gpt-6.1-sol", "--effort", "high")

	data, err := os.ReadFile(filepath.Join(root, ".smith", "repo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"\npresets:\n  codex:\n    agent: codex\n    model: gpt-6.1-sol\n    effort: high\n", "\nroles:\n  work: codex\n"} {
		if !strings.Contains(string(data), line) {
			t.Errorf("repo file = %q, want it to set %q", data, strings.TrimSpace(line))
		}
	}
}

func TestRepoInitRefusesOutsideAGitRepo(t *testing.T) {
	workdir := t.TempDir()

	_, stderr, code := runRepoInit(t, "", workdir, t.TempDir())

	if code == 0 || !strings.Contains(stderr, "must be run inside a git repo") {
		t.Errorf("exit code = %d, stderr = %q, want a refusal saying it must run inside a git repo", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".smith")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat .smith = %v, want nothing written", err)
	}
}

func TestRepoInitRefusesWhenTheReposSmithIsTheConfigHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".smith")
	writeFile(t, filepath.Join(home, "preferences.yaml"), "access: tailscale\n")

	_, stderr, code := runRepoInit(t, root, root, home)

	if code == 0 || !strings.Contains(stderr, home) {
		t.Errorf("exit code = %d, stderr = %q, want a refusal naming the config home %s", code, stderr, home)
	}
	if _, err := os.Stat(filepath.Join(home, "repo.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat repo.yaml = %v, want the config home unchanged", err)
	}
}

func TestRepoInitRefusesWhenTheReposSmithSymlinksToTheConfigHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(t.TempDir(), ".smith")
	writeFile(t, filepath.Join(home, "preferences.yaml"), "access: tailscale\n")
	if err := os.Symlink(home, filepath.Join(root, ".smith")); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runRepoInit(t, root, root, home)

	if code == 0 || !strings.Contains(stderr, home) {
		t.Errorf("exit code = %d, stderr = %q, want a refusal naming the config home %s", code, stderr, home)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "preferences.yaml" {
		t.Errorf("config home holds %v, want it unchanged", entries)
	}
}
