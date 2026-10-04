package loop

import (
	_ "embed"
	"strconv"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

//go:embed prompt.md
var builtinPrompt string

//go:embed interactive.md
var interactiveNote string

// Prompt is a loop prompt: the text handed to an agent for one task, with
// {{TASK_NUMBER}}, {{TASK_TITLE}} and {{SPEC_NUMBER}} placeholders.
type Prompt string

// BuiltinPrompt returns the loop prompt smith ships, placeholders unfilled.
func BuiltinPrompt() Prompt { return Prompt(builtinPrompt) }

// Interactive returns p with the interactive note appended, which asks the
// agent to work with the human attached to its terminal.
func (p Prompt) Interactive() Prompt {
	return Prompt(strings.TrimRight(string(p), "\n") + "\n\n" + interactiveNote)
}

// Render fills p's placeholders in with task and the number of its spec.
func (p Prompt) Render(task tracker.Task, spec int) string {
	return strings.NewReplacer(
		"{{TASK_NUMBER}}", strconv.Itoa(task.Number),
		"{{TASK_TITLE}}", task.Title,
		"{{SPEC_NUMBER}}", strconv.Itoa(spec),
	).Replace(string(p))
}
