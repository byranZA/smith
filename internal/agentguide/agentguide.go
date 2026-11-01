// Package agentguide renders the agent guide `smith help agents` prints.
package agentguide

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed guide.md
var prose string

// Verb describes one smith verb and the traits an agent must plan around.
type Verb struct {
	Path          string
	Summary       string
	Relays        bool
	RunsLong      bool
	NeedsTerminal bool
}

// ExitCode pairs a process exit code with what it means.
type ExitCode struct {
	Code    int
	Meaning string
}

// Render returns the agent guide as Markdown: the embedded prose, then the verb map and exit codes rendered from the given data.
func Render(verbs []Verb, codes []ExitCode) string {
	var b strings.Builder
	b.WriteString(prose)
	b.WriteString("\n## Verbs\n\n")
	for _, v := range verbs {
		fmt.Fprintf(&b, "- `%s`: %s%s\n", v.Path, v.Summary, traits(v))
	}
	b.WriteString("\n## Exit codes\n\n")
	for _, c := range codes {
		fmt.Fprintf(&b, "- `%d`: %s\n", c.Code, c.Meaning)
	}
	return b.String()
}

// traits renders a verb's traits as a parenthesised suffix, empty when it has none.
func traits(v Verb) string {
	var t []string
	if v.Relays {
		t = append(t, "relays to a box")
	}
	if v.RunsLong {
		t = append(t, "runs long")
	}
	if v.NeedsTerminal {
		t = append(t, "needs a terminal")
	}
	if len(t) == 0 {
		return ""
	}
	return " (" + strings.Join(t, "; ") + ")"
}
