package session

import (
	"fmt"
	"strings"
)

// startHint is the command an operator with no sessions runs next. A listing
// that only said "nothing here" would leave them to go and look it up.
const startHint = "Start one with:\n  smith session start --repo <name> --branch <name>"

// notYetCounted is what a column whose value this build does not compute
// renders as. It is the same dash a value that does not apply renders as,
// which is the honest reading either way: smith is not claiming a number.
const notYetCounted = "-"

// Readout renders the operator-facing listing of sessions: one flat row each,
// in the order they were handed, under the four columns the work-state
// question is answered in.
//
// It is never grouped, paginated or truncated — a group header breaks grep,
// awk, and pasting a name into the next command, and the identity column
// exists to be pasted. The true branch is deliberately absent: the name is
// what the other verbs take.
//
// The summary line is printed unconditionally, including when there is nothing
// to list, because the reassuring read before a hand-teardown only reassures if
// it cannot be absent. The DIRTY and UNPUSHED values are not computed by this
// build and render as dashes; their headers are here because the shape of the
// table is what an operator learns.
func Readout(sessions []Session) string {
	var b strings.Builder
	if len(sessions) == 0 {
		fmt.Fprintf(&b, "no sessions\n\n%s\n", startHint)
		return b.String()
	}

	width := len("NAME")
	for _, s := range sessions {
		width = max(width, len(s.Name))
	}
	writeRow(&b, width, "NAME", "STATE", "DIRTY", "UNPUSHED")
	for _, s := range sessions {
		writeRow(&b, width, s.Name, state(s), notYetCounted, notYetCounted)
	}
	fmt.Fprintf(&b, "\n%s\n", summary(len(sessions)))
	return b.String()
}

// state renders the STATE column: what a probe just found, in the two words
// the other verbs are written in terms of.
func state(s Session) string {
	if s.Live {
		return "live"
	}
	return "stopped"
}

// writeRow writes one listing row, padding the identity column so the columns
// line up under their headers.
func writeRow(b *strings.Builder, width int, name, state, dirty, unpushed string) {
	fmt.Fprintf(b, "%-*s  %-7s  %-5s  %s\n", width, name, state, dirty, unpushed)
}

// summary renders the count line for n listed sessions.
func summary(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}
