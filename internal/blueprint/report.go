package blueprint

import (
	"fmt"
	"strings"
)

// Finding is one thing wrong with a blueprint document, in the operator's own
// vocabulary. It names where the problem is — a line number where the parser
// supplies one, a structural path such as "repos[1]" where the rule is
// semantic — and what is wrong with it.
type Finding struct {
	// Line is the line the problem appears on, or 0 when the rule that found
	// it has no line to point at.
	Line int
	// Path is the structural path of the block the finding is about, such as
	// "repos[1]" or "repos[1].placements[0].to". It is empty at the document
	// root.
	Path string
	// Message states what is wrong, naming no Go type and no package.
	Message string
}

// String renders one finding as a single line of the report.
func (f Finding) String() string {
	switch {
	case f.Line > 0 && f.Path != "":
		return fmt.Sprintf("line %d: %s (%s)", f.Line, f.Message, f.Path)
	case f.Line > 0:
		return fmt.Sprintf("line %d: %s", f.Line, f.Message)
	case f.Path != "":
		return fmt.Sprintf("%s: %s", f.Path, f.Message)
	default:
		return f.Message
	}
}

// ValidationError is everything wrong with one blueprint document, gathered in
// a single pass so an operator fixing a blueprint never plays whack-a-mole.
// Its findings are ordered as they appear in the document.
type ValidationError struct {
	// Findings are the problems found, in document order. There is always at
	// least one.
	Findings []Finding
	// Malformed reports that the document is not YAML at all. A malformed
	// document has one finding — the position the parser gave up at — and no
	// schema was ever applied to it.
	Malformed bool
}

// Error renders the operator-facing report: one line per finding, or a single
// distinctly worded line when the document is malformed.
func (e *ValidationError) Error() string {
	if e.Malformed {
		return "blueprint is malformed: " + e.Findings[0].String()
	}
	var b strings.Builder
	b.WriteString("blueprint is invalid:")
	for _, f := range e.Findings {
		b.WriteString("\n  ")
		b.WriteString(f.String())
	}
	return b.String()
}
