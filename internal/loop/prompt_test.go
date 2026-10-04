package loop_test

import (
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
