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
// from is the caller's assertion that the entry under it is this very box's:
// the entry is moved, and moving one that belongs to another machine would take
// that box's name off it. A caller holding a name off a box's marker has
// PreviousName decide that from the inventory rather than asserting it.
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

// PreviousName returns the name Rename should move this box's entry off, and ""
// when there is none to move: the name the box's own marker records, and only
// when the entry under that name already reaches this very box — over the
// address the run came in on, or over the target it is about to register.
//
// It is what separates a re-run from a collision. Setup stamps the name it
// resolved onto the box's marker before it registers, so a box whose
// registration was refused for a name another machine holds still records that
// name. The marker alone therefore cannot tell "this box's address changed"
// from "that name belongs to a different box"; the inventory's own target can.
// A name whose entry points at another machine is not this box's to move, so
// the registration falls through to the collision rule and is refused rather
// than stealing the entry — including when the operator recovers by passing a
// free --name, which must leave the held name where it is.
func PreviousName(inv Inventory, recorded, addressed, target string) string {
	entry, ok := inv.Boxes[recorded]
	if recorded == "" || !ok {
		return ""
	}
	if entry.Target != addressed && entry.Target != target {
		return ""
	}
	return recorded
}
