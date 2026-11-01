package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/agentguide"
)

// guideCommandLines returns each `smith …` line the guide prose shows in a fenced sh block.
func guideCommandLines(prose string) []string {
	var lines []string
	in := false
	for _, line := range strings.Split(prose, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			in = strings.HasPrefix(line, "```sh")
		case in && strings.HasPrefix(line, "smith "):
			lines = append(lines, line)
		}
	}
	return lines
}

// checkCommandLine reports why a `smith …` line does not parse against root: an unknown verb, or a flag the verb lacks. Words in <angle brackets> and positional arguments are not checked.
func checkCommandLine(root *cobra.Command, line string) string {
	line, _, _ = strings.Cut(line, " #")
	words := strings.Fields(line)[1:]
	cmd, _, err := root.Find(words)
	if err != nil || cmd == root || !cmd.Runnable() {
		return "names no verb smith has"
	}
	for _, w := range words {
		if !strings.HasPrefix(w, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(w, "-"), "=")
		if strings.HasPrefix(w, "--") {
			if cmd.Flag(name) == nil {
				return "names the flag --" + name + ", which " + cmd.CommandPath() + " lacks"
			}
		} else if cmd.Flags().ShorthandLookup(name) == nil && cmd.InheritedFlags().ShorthandLookup(name) == nil {
			return "names the flag -" + name + ", which " + cmd.CommandPath() + " lacks"
		}
	}
	return ""
}

func TestGuideCommandLinesParseAgainstTheCommandTree(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	lines := guideCommandLines(agentguide.Prose())
	if len(lines) < 10 {
		t.Fatalf("the guide shows %d smith command lines, want at least 10", len(lines))
	}
	for _, line := range lines {
		if why := checkCommandLine(root, line); why != "" {
			t.Errorf("guide line %q %s", line, why)
		}
	}
}

func TestCommandLineCheckCatchesAMissingVerbOrFlag(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	for _, line := range []string{"smith teleport dev", "smith loop run dev 42 --no-such-flag", "smith loop run -Z"} {
		if checkCommandLine(root, line) == "" {
			t.Errorf("checkCommandLine(%q) accepted a line smith cannot run", line)
		}
	}
	if why := checkCommandLine(root, "smith loop run dev 42 --repo app # go"); why != "" {
		t.Errorf("checkCommandLine rejected a good line: %s", why)
	}
}
