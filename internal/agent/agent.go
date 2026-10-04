// Package agent holds smith's agent adapters: its built-in knowledge of each
// coding-agent CLI and how to run it. An adapter only builds a command line;
// it never starts a process.
package agent

import (
	"fmt"
	"slices"
	"strings"
)

// Command is a coding-agent invocation: the program, its arguments, and the
// environment variables to add to smith's own.
type Command struct {
	Name string
	Args []string
	Env  []string
}

// Options are what a run asks of the agent beyond its prompt. A field left
// empty is unset, and the agent uses its own default for it.
type Options struct {
	// Model is the model the agent runs, passed to it verbatim.
	Model string
	// Effort is how hard the agent thinks, on smith's own scale.
	Effort Effort
}

// Adapter knows how to run one coding-agent CLI.
type Adapter interface {
	// Unattended returns the command that runs the agent headless and fully
	// autonomous on prompt, asking the operator nothing.
	Unattended(prompt string, opts Options) Command
}

// Default is the agent the loop runs when nothing names one.
const Default = "claude"

// adapters is every agent smith knows, by the name configuration uses for it.
var adapters = map[string]Adapter{
	"claude": claude{},
}

// UnknownError reports an agent name smith has no adapter for.
type UnknownError struct {
	Name string
}

// Error implements error.
func (e *UnknownError) Error() string {
	return fmt.Sprintf("unknown agent %q: known agents are %s", e.Name, strings.Join(Known(), ", "))
}

// Lookup returns the adapter for the agent called name.
func Lookup(name string) (Adapter, error) {
	adapter, ok := adapters[name]
	if !ok {
		return nil, &UnknownError{Name: name}
	}
	return adapter, nil
}

// Known returns the names of every agent smith has an adapter for, sorted.
func Known() []string {
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// claude is the adapter for Anthropic's Claude Code CLI.
type claude struct{}

// Unattended runs claude headless in auto permission mode, so it asks for no
// approval, with background tasks disabled so nothing outlives the run.
// claude's own --effort scale includes smith's, so effort passes through.
func (claude) Unattended(prompt string, opts Options) Command {
	args := []string{"--permission-mode", "auto"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", string(opts.Effort))
	}
	return Command{
		Name: "claude",
		Args: append(args, "--print", prompt),
		Env:  []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1"},
	}
}
