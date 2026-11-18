package loop_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func TestBuiltinPromptsCarryTheToolsPlaceholder(t *testing.T) {
	t.Parallel()
	if !strings.Contains(string(loop.BuiltinPrompt()), "{{SMITH_TOOLS}}") {
		t.Error("the built-in work prompt has no {{SMITH_TOOLS}}")
	}
	if !strings.Contains(string(loop.BuiltinReviewPrompt()), "{{SMITH_TOOLS}}") {
		t.Error("the built-in review prompt has no {{SMITH_TOOLS}}")
	}
}

func TestWithToolsFillsThePlaceholderWithTheSection(t *testing.T) {
	t.Parallel()
	got := loop.Prompt("a\n\n{{SMITH_TOOLS}}\n\nb\n").WithTools("## Smith tools\n\nx\n").Render(tracker.Task{}, "")
	if want := "a\n\n## Smith tools\n\nx\n\nb\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestEmptyToolsSectionLeavesNoGapOrHeading(t *testing.T) {
	t.Parallel()
	got := loop.Prompt("a\n\n{{SMITH_TOOLS}}\n\nb\n").WithTools("").Render(tracker.Task{}, "")
	if want := "a\n\nb\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestReviewPromptWithoutToolsDropsThePlaceholder(t *testing.T) {
	t.Parallel()
	got := loop.ReviewPrompt("a\n\n{{SMITH_TOOLS}}\n\nb\n").Render(loop.ReviewTarget{})
	if want := "a\n\nb\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestRunWithAStackListsStackUpInTheWorkPrompt(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.ToolsDir = toolsDir(t)
	l.Prompt = loop.BuiltinPrompt()

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	args := j.board.launched[0].Args
	prompt := args[len(args)-1]
	for _, want := range []string{"## Smith tools", "`smith tool stack-up`", "image", "compose", "environment", "Code reloading is the repo's job"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

func TestRunWithNoStackLeavesNoSmithToolsHeadingInThePrompt(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.ToolsDir = toolsDir(t)
	l.Prompt = loop.BuiltinPrompt()

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	args := b.launched[0].Args
	if prompt := args[len(args)-1]; strings.Contains(prompt, "Smith tools") || strings.Contains(prompt, "{{") {
		t.Errorf("prompt holds a tools section or placeholder:\n%s", prompt)
	}
}

func TestRunNotesAnEjectedPromptWithoutThePlaceholderAndStillServesTools(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.ToolsDir = toolsDir(t)
	var progress bytes.Buffer
	l.Progress = &progress
	probe := &toolsProbe{next: j}
	l.Launcher = probe

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := strings.Count(progress.String(), "{{SMITH_TOOLS}} placeholder"); got != 1 {
		t.Errorf("notice count = %d in:\n%s", got, progress.String())
	}
	if len(probe.paths) != 1 || probe.paths[0] == "" {
		t.Errorf("SMITH_TOOLS per launch = %q, want a socket path", probe.paths)
	}
}

func TestRunWithTheBuiltinPromptPrintsNoPlaceholderNotice(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.ToolsDir = toolsDir(t)
	l.Prompt = loop.BuiltinPrompt()
	var progress bytes.Buffer
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if strings.Contains(progress.String(), "placeholder") {
		t.Errorf("unexpected notice in:\n%s", progress.String())
	}
}
