package inventory

import (
	"errors"
	"fmt"
)

// NameCollisionError reports a registration refused because the name is
// already held by a different box. It carries both sides, because what the
// operator has to decide between is the box they are registering and the one
// the name already reaches.
type NameCollisionError struct {
	// Name is the box name both boxes want.
	Name string
	// Registered is the target the name already reaches.
	Registered string
	// Wanted is the target the refused registration would have stored.
	Wanted string
}

// Error implements error.
func (e *NameCollisionError) Error() string {
	return fmt.Sprintf(
		"%q already names a different box (%s): register this one under another name with --name, "+
			"or forget that one first",
		e.Name, e.Registered)
}

// Register returns a copy of inv with name mapped to target. It is a hard
// error when the name already reaches a different box: a silent "dev-2" would
// leave the operator typing a name that means something they never chose.
//
// The same name mapping to the same target is an idempotent success, which is
// what makes re-running setup and re-running add both safe. Two names for one
// box are legal — this is a name-to-target map, with no uniqueness on targets.
//
// It does not mutate inv.
func Register(inv Inventory, name, target string) (Inventory, error) {
	if err := validate(name, target); err != nil {
		return inv, err
	}
	if err := collision(inv, name, target); err != nil {
		return inv, err
	}
	return put(inv, name, target), nil
}

// validate rejects the two registrations that name nothing: a box has a name
// and an address, and an empty one of either is a caller's bug rather than an
// operator's choice.
func validate(name, target string) error {
	if name == "" {
		return errors.New("no box name: a box is registered under a name, not an empty string")
	}
	if target == "" {
		return fmt.Errorf("no target for box %q: a box is registered by the address smith reaches it over", name)
	}
	return nil
}

// collision reports the refusal a registration under name is owed when the
// name already reaches a different box, and nil when the name is free or
// already reaches this very target.
func collision(inv Inventory, name, target string) error {
	if existing, ok := inv.Boxes[name]; ok && existing.Target != target {
		return &NameCollisionError{Name: name, Registered: existing.Target, Wanted: target}
	}
	return nil
}

// put returns a copy of inv with name mapped to target, having decided nothing.
func put(inv Inventory, name, target string) Inventory {
	next := clone(inv)
	next.Boxes[name] = Box{Target: target}
	return next
}

// clone returns a copy of inv sharing none of its map, so a caller's inventory
// is never changed by what it was used to compute.
func clone(inv Inventory) Inventory {
	boxes := make(map[string]Box, len(inv.Boxes)+1)
	for name, box := range inv.Boxes {
		boxes[name] = box
	}
	return Inventory{SchemaVersion: inv.SchemaVersion, Boxes: boxes}
}
