package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func TestRenderFillsInTheTaskAndItsSpec(t *testing.T) {
	prompt := loop.Prompt("Do #{{TASK_NUMBER}} ({{TASK_TITLE}}) of spec #{{SPEC_NUMBER}}; close #{{TASK_NUMBER}}.")

	got := prompt.Render(tracker.Task{Number: 43, Title: "Loop: run"}, 42)

	if want := "Do #43 (Loop: run) of spec #42; close #43."; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestTheBuiltInPromptNamesTheTaskAndSpec(t *testing.T) {
	got := loop.BuiltinPrompt().Render(tracker.Task{Number: 43, Title: "Loop: run"}, 42)

	for _, want := range []string{"#43", "Loop: run", "#42"} {
		if !strings.Contains(got, want) {
			t.Errorf("built-in prompt is missing %q", want)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("built-in prompt left a placeholder unfilled:\n%s", got)
	}
}

func TestAnInteractivePromptCarriesTheInteractiveNoteAfterTheTask(t *testing.T) {
	got := loop.Prompt("Do #{{TASK_NUMBER}}.").Interactive().Render(tracker.Task{Number: 43}, 42)

	if !strings.HasPrefix(got, "Do #43.\n") || !strings.Contains(got, "A human is at the terminal") {
		t.Errorf("interactive prompt = %q, want the task followed by the interactive note", got)
	}
}

func TestAnEjectedPromptReplacesTheBuiltInWithItsPlaceholdersFilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("Do #{{TASK_NUMBER}} of #{{SPEC_NUMBER}}. Run make check before closing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, err := loop.LoadPrompt(path)
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if got, want := prompt.Render(tracker.Task{Number: 43}, 42), "Do #43 of #42. Run make check before closing.\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestWithoutAnEjectedPromptTheBuiltInIsUsed(t *testing.T) {
	prompt, err := loop.LoadPrompt(filepath.Join(t.TempDir(), "prompt.md"))
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if prompt != loop.BuiltinPrompt() {
		t.Errorf("LoadPrompt() = %q, want the built-in prompt", prompt)
	}
}

func TestAnEjectedCopyOfTheBuiltInRendersTheSameAsTheBuiltIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte(loop.BuiltinPrompt()), 0o644); err != nil {
		t.Fatal(err)
	}
	task := tracker.Task{Number: 43, Title: "Loop: run"}

	prompt, err := loop.LoadPrompt(path)
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if got, want := prompt.Interactive().Render(task, 42), loop.BuiltinPrompt().Interactive().Render(task, 42); got != want {
		t.Errorf("ejected copy renders\n%s\nwant\n%s", got, want)
	}
}

func TestAnUnknownPlaceholderInAnEjectedPromptIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("Do #{{TASK_NUMBER}}, see {{ISSUE_URL}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loop.LoadPrompt(path)

	want := []string{path, "{{ISSUE_URL}}", "{{TASK_NUMBER}}, {{TASK_TITLE}}, {{SPEC_NUMBER}}"}
	for _, w := range want {
		if err == nil || !strings.Contains(err.Error(), w) {
			t.Errorf("LoadPrompt() error = %v, want one naming %q", err, w)
		}
	}
}
