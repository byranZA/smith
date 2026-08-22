package inventory

// Rename returns a copy of inv with the box at target registered as to and no
// longer registered as from. It is what an operator means by passing a --name
// that differs from the name the box already records: the entry moves, rather
// than a second name for one machine appearing beside the first.
//
// A from that is not registered is not an error — the box may never have been
// in the inventory, or the operator may have deleted the file — so the rename
// degenerates to a plain registration. An empty from is the same case, and is
// how a verb with no previous name to move records a box.
//
// The collision rule is Register's, with one exception the rename needs: a to
// that already reaches the very box being renamed is not a collision, so
// re-running setup on a registered box whose address changed updates its one
// entry in place instead of being refused by it.
//
// It does not mutate inv.
func Rename(inv Inventory, from, to, target string) (Inventory, error) {
	if err := validate(to, target); err != nil {
		return inv, err
	}
	if !sameBox(inv, from, to) {
		if err := collision(inv, to, target); err != nil {
			return inv, err
		}
	}
	next := put(inv, to, target)
	if from != "" && from != to {
		delete(next.Boxes, from)
	}
	return next, nil
}

// sameBox reports whether the entry to names, if any, is the very box being
// renamed: the same name, or a second name already pointing at the same
// target. Identity in the inventory is the target, so a name that reaches this
// machine is never a different box's.
func sameBox(inv Inventory, from, to string) bool {
	if from == "" {
		return false
	}
	if from == to {
		return true
	}
	previous, ok := inv.Boxes[from]
	if !ok {
		return false
	}
	existing, ok := inv.Boxes[to]
	return ok && existing.Target == previous.Target
}
