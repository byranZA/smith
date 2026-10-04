package loop

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/byranZA/smith/internal/tracker"
)

//go:embed prompt.md
var builtinPrompt string

//go:embed interactive.md
var interactiveNote string

// Prompt is a loop prompt: the text handed to an agent for one task, with
// {{TASK_NUMBER}} and {{TASK_TITLE}} placeholders.
type Prompt string

// placeholders are the placeholders a loop prompt may hold, in the order a
// refusal lists them.
var placeholders = []string{"{{TASK_NUMBER}}", "{{TASK_TITLE}}"}

// placeholderPattern matches anything written as a placeholder, known or not.
var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// LoadPrompt reads the loop prompt a repo ejected to path, falling back to the
// built-in prompt when there is none. A prompt holding a placeholder smith does
// not fill is refused, naming it and the known placeholders. It never writes.
func LoadPrompt(path string) (Prompt, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return BuiltinPrompt(), nil
	}
	if err != nil {
		return "", fmt.Errorf("read loop prompt: %w", err)
	}
	for _, found := range placeholderPattern.FindAllString(string(data), -1) {
		if !slices.Contains(placeholders, found) {
			return "", fmt.Errorf("%s: unknown placeholder %s: known placeholders are %s", path, found, strings.Join(placeholders, ", "))
		}
	}
	return Prompt(data), nil
}

// BuiltinPrompt returns the loop prompt smith ships, placeholders unfilled.
func BuiltinPrompt() Prompt { return Prompt(builtinPrompt) }

// Interactive returns p with the interactive note appended, which asks the
// agent to work with the human attached to its terminal.
func (p Prompt) Interactive() Prompt {
	return Prompt(strings.TrimRight(string(p), "\n") + "\n\n" + interactiveNote)
}

// Render fills p's placeholders in with task.
func (p Prompt) Render(task tracker.Task) string {
	return strings.NewReplacer(
		"{{TASK_NUMBER}}", strconv.Itoa(task.Number),
		"{{TASK_TITLE}}", task.Title,
	).Replace(string(p))
}
