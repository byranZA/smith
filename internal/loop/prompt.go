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

//go:embed smith_tools.md
var smithToolsSection string

// toolsPlaceholder marks where a prompt lists the smith tools its run serves.
const toolsPlaceholder = "{{SMITH_TOOLS}}"

// fillTools returns text with the tools placeholder replaced by section, or,
// when section is empty, removed along with the blank line after it.
func fillTools(text, section string) string {
	if section == "" {
		text = strings.ReplaceAll(text, toolsPlaceholder+"\n\n", "")
	}
	return strings.ReplaceAll(text, toolsPlaceholder, strings.TrimRight(section, "\n"))
}

// Prompt is a loop prompt: the text handed to an agent for one task, with
// {{TASK_NUMBER}}, {{TASK_TITLE}} and {{REPORT_FILE}} placeholders.
type Prompt string

// placeholders are the placeholders a loop prompt may hold, in the order a
// refusal lists them.
var placeholders = []string{"{{TASK_NUMBER}}", "{{TASK_TITLE}}", "{{REPORT_FILE}}", toolsPlaceholder}

// placeholderPattern matches anything written as a placeholder, known or not.
var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// LoadPrompt reads the loop prompt a repo ejected to path, falling back to the
// built-in prompt when there is none, and refuses a placeholder smith does not fill.
func LoadPrompt(path string) (Prompt, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return BuiltinPrompt(), nil
	}
	if err != nil {
		return "", fmt.Errorf("read loop prompt: %w", err)
	}
	return ParsePrompt(path, data)
}

// ParsePrompt takes data as an ejected loop prompt read from source, refusing
// a placeholder smith does not fill and naming source.
func ParsePrompt(source string, data []byte) (Prompt, error) {
	for _, found := range placeholderPattern.FindAllString(string(data), -1) {
		if !slices.Contains(placeholders, found) {
			return "", fmt.Errorf("%s: unknown placeholder %s: known placeholders are %s", source, found, strings.Join(placeholders, ", "))
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

// WithTools returns p with the tools placeholder filled in with section, the
// text listing the smith tools a run serves, empty when it serves none.
func (p Prompt) WithTools(section string) Prompt { return Prompt(fillTools(string(p), section)) }

// Render fills p's placeholders in with task and the path reportFile the
// agent writes its attempt report to. A tools placeholder WithTools has not
// filled expands to nothing.
func (p Prompt) Render(task tracker.Task, reportFile string) string {
	return strings.NewReplacer(
		"{{TASK_NUMBER}}", strconv.Itoa(task.Number),
		"{{TASK_TITLE}}", task.Title,
		"{{REPORT_FILE}}", reportFile,
	).Replace(fillTools(string(p), ""))
}
