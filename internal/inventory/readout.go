package inventory

import (
	"fmt"
	"sort"
	"strings"
)

// registerHint is the command an operator with nothing registered runs next.
// An empty listing that only said "nothing here" would leave them to go and
// look it up.
const registerHint = "Register one with:\n  smith machine add <login>@<host>"

// Readout renders the operator-facing listing of an inventory: the boxes in
// name order with their targets, and a summary line that is printed
// unconditionally — including when there is nothing to list, because an
// unconditional line is the only kind an operator learns to trust.
//
// reach maps a box's name to whether a probe just reached it, and adds the
// REACHABLE column. It is nil when no probe ran, which is the ordinary case:
// reachability is a display concern, never a stored one, so it only ever
// arrives alongside the listing and never from the file.
//
// It is flat, never grouped, paginated or truncated. A skew this build does not
// fully understand is noted after the summary rather than withheld: the entries
// smith recognised are still worth listing, and the operator is owed the reason
// the file may hold more.
func Readout(inv Inventory, skew Skew, reach map[string]bool) string {
	var b strings.Builder
	names := sortedNames(inv)
	if len(names) == 0 {
		fmt.Fprintf(&b, "no boxes registered\n\n%s\n", registerHint)
		writeNote(&b, skew)
		return b.String()
	}

	nameWidth, targetWidth := len("NAME"), len("TARGET")
	for _, name := range names {
		nameWidth = max(nameWidth, len(name))
		targetWidth = max(targetWidth, len(inv.Boxes[name].Target))
	}
	writeRow(&b, nameWidth, targetWidth, "NAME", "TARGET", header(reach))
	for _, name := range names {
		writeRow(&b, nameWidth, targetWidth, name, inv.Boxes[name].Target, cell(reach, name))
	}
	fmt.Fprintf(&b, "\n%s\n", summary(len(names)))
	writeNote(&b, skew)
	return b.String()
}

// header returns the third column's heading, empty when no probe ran.
func header(reach map[string]bool) string {
	if reach == nil {
		return ""
	}
	return "REACHABLE"
}

// cell returns the reachability a probe found for the named box. A box the
// probe results say nothing about renders empty rather than guessing, since
// "smith did not ask" is not the same answer as "the box did not reply".
func cell(reach map[string]bool, name string) string {
	reachable, probed := reach[name]
	switch {
	case !probed:
		return ""
	case reachable:
		return "reachable"
	default:
		return "unreachable"
	}
}

// writeRow writes one listing row, dropping the trailing column (and the
// padding before it) when there is nothing in it to print.
func writeRow(b *strings.Builder, nameWidth, targetWidth int, name, target, reach string) {
	if reach == "" {
		fmt.Fprintf(b, "%-*s  %s\n", nameWidth, name, target)
		return
	}
	fmt.Fprintf(b, "%-*s  %-*s  %s\n", nameWidth, name, targetWidth, target, reach)
}

// sortedNames returns the inventory's box names in name order, the one order
// the listing is ever printed in.
func sortedNames(inv Inventory) []string {
	names := make([]string, 0, len(inv.Boxes))
	for name := range inv.Boxes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// summary renders the count line for n listed boxes.
func summary(n int) string {
	if n == 1 {
		return "1 box"
	}
	return fmt.Sprintf("%d boxes", n)
}

// writeNote appends the skew's note, if it has one to make.
func writeNote(b *strings.Builder, skew Skew) {
	if note := skew.Note(); note != "" {
		fmt.Fprintf(b, "\n%s\n", note)
	}
}
