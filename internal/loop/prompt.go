package loop

import (
	_ "embed"
	"strconv"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

//go:embed prompt.md
var builtinPrompt string

// Prompt is a loop prompt: the text handed to an agent for one task, with
// {{TASK_NUMBER}}, {{TASK_TITLE}} and {{SPEC_NUMBER}} placeholders.
type Prompt string

// BuiltinPrompt returns the loop prompt smith ships, placeholders unfilled.
func BuiltinPrompt() Prompt { return Prompt(builtinPrompt) }

// Render fills p's placeholders in with task and the number of its spec.
func (p Prompt) Render(task tracker.Task, spec int) string {
	return strings.NewReplacer(
		"{{TASK_NUMBER}}", strconv.Itoa(task.Number),
		"{{TASK_TITLE}}", task.Title,
		"{{SPEC_NUMBER}}", strconv.Itoa(spec),
	).Replace(string(p))
}
