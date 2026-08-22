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
// It is flat, never grouped, paginated or truncated. A skew this build does not
// fully understand is noted after the summary rather than withheld: the entries
// smith recognised are still worth listing, and the operator is owed the reason
// the file may hold more.
func Readout(inv Inventory, skew Skew) string {
	var b strings.Builder
	names := sortedNames(inv)
	if len(names) == 0 {
		fmt.Fprintf(&b, "no boxes registered\n\n%s\n", registerHint)
		writeNote(&b, skew)
		return b.String()
	}

	width := len("NAME")
	for _, name := range names {
		if len(name) > width {
			width = len(name)
		}
	}
	fmt.Fprintf(&b, "%-*s  %s\n", width, "NAME", "TARGET")
	for _, name := range names {
		fmt.Fprintf(&b, "%-*s  %s\n", width, name, inv.Boxes[name].Target)
	}
	fmt.Fprintf(&b, "\n%s\n", summary(len(names)))
	writeNote(&b, skew)
	return b.String()
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
