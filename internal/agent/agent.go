// Package agent holds smith's agent adapters: its built-in knowledge of each
// coding-agent CLI and how to run it. An adapter only builds a command line;
// it never starts a process.
package agent

import (
	"fmt"
	"slices"
	"strings"
)

// Command is a coding-agent invocation: the program, its arguments, the
// environment variables to add to smith's own, and whether it is attached to
// the operator's terminal.
type Command struct {
	Name string
	Args []string
	Env  []string
	// Attached commands read the operator's terminal and own it until they
	// exit; the rest read no input.
	Attached bool
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
	// Interactive returns the command that runs the agent's own interface
	// attached to the operator's terminal, starting on prompt.
	Interactive(prompt string, opts Options) Command
}

// Default is the agent the loop runs when nothing names one.
const Default = "claude"

// adapters is every agent smith knows, by the name configuration uses for it.
var adapters = map[string]Adapter{
	"claude": claude{},
	"codex":  codex{},
	"pi":     pi{},
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
func (c claude) Unattended(prompt string, opts Options) Command {
	args := append([]string{"--permission-mode", "auto"}, c.options(opts)...)
	return Command{
		Name: "claude",
		Args: append(args, "--print", prompt),
		Env:  []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1"},
	}
}

// Interactive runs claude's own interface on the operator's terminal with
// prompt as its first message, leaving approvals to the operator.
func (c claude) Interactive(prompt string, opts Options) Command {
	return Command{
		Name:     "claude",
		Args:     append(c.options(opts), prompt),
		Attached: true,
	}
}

// options are claude's flags for the model and effort opts set, in claude's
// own terms: the model verbatim, and an --effort scale that includes smith's.
func (claude) options(opts Options) []string {
	var args []string
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", string(opts.Effort))
	}
	return args
}

// codex is the adapter for OpenAI's Codex CLI.
type codex struct{}

// Unattended runs codex exec with --yolo, which bypasses every approval and
// the sandbox, so it asks the operator nothing.
func (c codex) Unattended(prompt string, opts Options) Command {
	args := append([]string{"exec", "--yolo"}, c.options(opts)...)
	return Command{
		Name: "codex",
		Args: append(args, prompt),
	}
}

// Interactive runs codex's own interface on the operator's terminal with
// prompt as its first message, leaving approvals to the operator.
func (c codex) Interactive(prompt string, opts Options) Command {
	return Command{
		Name:     "codex",
		Args:     append(c.options(opts), prompt),
		Attached: true,
	}
}

// options are codex's flags for the model and effort opts set, in codex's
// own terms: the model verbatim, and its model_reasoning_effort setting, whose
// scale includes smith's.
func (codex) options(opts Options) []string {
	var args []string
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", opts.Effort))
	}
	return args
}

// pi is the adapter for the pi coding agent.
type pi struct{}

// Unattended runs pi in print mode, which processes prompt and exits; pi has
// no approval prompts to bypass.
func (p pi) Unattended(prompt string, opts Options) Command {
	return Command{
		Name: "pi",
		Args: append(p.options(opts), "--print", prompt),
	}
}

// Interactive runs pi's own interface on the operator's terminal with prompt
// as its first message.
func (p pi) Interactive(prompt string, opts Options) Command {
	return Command{
		Name:     "pi",
		Args:     append(p.options(opts), prompt),
		Attached: true,
	}
}

// options are pi's flags for the model and effort opts set, in pi's own
// terms: the model verbatim, and a --thinking scale that includes smith's.
func (pi) options(opts Options) []string {
	var args []string
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--thinking", string(opts.Effort))
	}
	return args
}
