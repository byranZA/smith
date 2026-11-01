package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

const pointerLine = "This repo's tasks are driven by smith: read `.smith/README.md` before running smith.\n"

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRepoInitAnsweringYesAppendsTheLineToThePlannedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "a\n")
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "c\n")
	term := &scriptedTerminal{attended: true, answer: "y"}

	_, stderr, code := runRepoInitAt(t, term, root, root, t.TempDir())

	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(term.question, "AGENTS.md and CLAUDE.md") || term.asks != 1 {
		t.Errorf("question = %q asked %d times, want one naming AGENTS.md and CLAUDE.md", term.question, term.asks)
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md"} {
		want := map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "c\n"}[f] + pointerLine
		if got := readFile(t, filepath.Join(root, f)); got != want {
			t.Errorf("%s = %q, want %q", f, got, want)
		}
	}
}

func TestRepoInitWithNoInstructionFileCreatesAgentsMD(t *testing.T) {
	root := t.TempDir()

	_, _, code := runRepoInitAt(t, &scriptedTerminal{attended: true, answer: "yes"}, root, root, t.TempDir())

	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != pointerLine {
		t.Errorf("AGENTS.md = %q, want %q", got, pointerLine)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md was created")
	}
}

func TestRepoInitAnsweringNoChangesNothingAndPrintsTheLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "a\n")

	stdout, _, code := runRepoInitAt(t, &scriptedTerminal{attended: true, answer: ""}, root, root, t.TempDir())

	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != "a\n" {
		t.Errorf("AGENTS.md = %q, want unchanged", got)
	}
	if !strings.Contains(stdout, pointerLine) {
		t.Errorf("stdout = %q, want the pointer line", stdout)
	}
}

func TestRepoInitWithNobodyAtTheTerminalPrintsTheLineAndTheCommand(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "a\n")
	term := &scriptedTerminal{attended: false}

	stdout, _, _ := runRepoInitAt(t, term, root, root, t.TempDir())

	if term.asked {
		t.Error("asked with nobody at the terminal")
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != "a\n" {
		t.Errorf("AGENTS.md = %q, want unchanged", got)
	}
	if !strings.Contains(stdout, pointerLine) || !strings.Contains(stdout, "smith repo init --yes") {
		t.Errorf("stdout = %q, want the line and the command", stdout)
	}
}

func TestRepoInitYesAddsTheLineUnattended(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "a\n")
	term := &scriptedTerminal{attended: false}
	cmd := newRepoCmd(func() (config.Home, error) { return config.NewHome(t.TempDir()), nil }, toplevelGit{root: root}, func() (string, error) { return root, nil }, term)

	_, stderr, code := underRootWith(t, cmd, "--yes", "repo", "init")

	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if term.asked {
		t.Error("asked despite --yes")
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != "a\n"+pointerLine {
		t.Errorf("AGENTS.md = %q, want the line added", got)
	}
}

func TestRepoInitAgainOnAPointedRepoAsksNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "a\n")
	runRepoInitAt(t, &scriptedTerminal{attended: true, answer: "y"}, root, root, t.TempDir())
	term := &scriptedTerminal{attended: true, answer: "y"}

	stdout, _, _ := runRepoInitAt(t, term, root, root, t.TempDir())

	if term.asked {
		t.Error("asked on a repo already pointed")
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != "a\n"+pointerLine {
		t.Errorf("AGENTS.md = %q, want the line once", got)
	}
	if strings.Contains(stdout, "pointed") {
		t.Errorf("stdout = %q, want no pointer report", stdout)
	}
}
