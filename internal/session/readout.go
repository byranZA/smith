package session

import (
	"fmt"
	"strconv"
	"strings"
)

// startHint is the command an operator with no sessions runs next. A listing
// that only said "nothing here" would leave them to go and look it up.
const startHint = "Start one with:\n  smith session start --repo <name> --branch <name>"

// notApplicable is what a column with nothing to report renders as: a worktree
// that is not dirty, and a detached worktree that has no branch to count
// commits on. The dash is the honest reading either way — smith is not
// claiming a number.
const notApplicable = "-"

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
// it cannot be absent.
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
		writeRow(&b, width, s.Name, state(s), dirty(s), unpushed(s))
	}
	fmt.Fprintf(&b, "\n%s\n", summary(sessions))
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

// dirty renders the DIRTY column: the one word an operator scans for, or a
// dash when there is nothing uncommitted to report.
func dirty(s Session) string {
	if s.Dirty {
		return "dirty"
	}
	return notApplicable
}

// unpushed renders the UNPUSHED column: the count of commits no remote has. A
// detached worktree is on no branch, so there is no count to make and the
// column says so rather than reporting a zero it did not compute.
func unpushed(s Session) string {
	if s.Branch == "" {
		return notApplicable
	}
	return strconv.Itoa(s.Unpushed)
}

// writeRow writes one listing row, padding the identity column so the columns
// line up under their headers.
func writeRow(b *strings.Builder, width int, name, state, dirty, unpushed string) {
	fmt.Fprintf(b, "%-*s  %-7s  %-5s  %s\n", width, name, state, dirty, unpushed)
}

// summary renders the line an operator reads before tearing a box down by
// hand: how many sessions there are and what of their work is at risk. A
// clause for a count of zero is left out, and everything being clean is said
// plainly rather than as two zeroes the reader has to interpret.
func summary(sessions []Session) string {
	clauses := []string{fmt.Sprintf("%d sessions", len(sessions))}
	if len(sessions) == 1 {
		clauses[0] = "1 session"
	}
	var dirty, unpushed int
	for _, s := range sessions {
		if s.Dirty {
			dirty++
		}
		if s.Branch != "" && s.Unpushed > 0 {
			unpushed++
		}
	}
	if dirty == 0 && unpushed == 0 {
		return clauses[0] + " · all clean"
	}
	if dirty > 0 {
		clauses = append(clauses, fmt.Sprintf("%d dirty", dirty))
	}
	if unpushed > 0 {
		clauses = append(clauses, fmt.Sprintf("%d with unpushed commits", unpushed))
	}
	return strings.Join(clauses, " · ")
}
