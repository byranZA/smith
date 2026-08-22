package inventory

import "fmt"

// UnknownBoxError reports a removal refused because no box is registered under
// the name. It carries the name, because that is the one thing the operator has
// to correct — and forgetting something that was never there is worth saying
// out loud rather than reporting as a success.
type UnknownBoxError struct {
	// Name is the box name that reaches nothing.
	Name string
}

// Error implements error.
func (e *UnknownBoxError) Error() string {
	return fmt.Sprintf("no box named %q is registered: see what is with smith machine list", e.Name)
}

// Forget returns a copy of inv with name no longer registered. Forgetting is
// the only removal there is, and it removes nothing but a line in a file: the
// box itself is never reached, which is what makes it safe to run and cheap to
// undo with `machine add`.
//
// A name that is not registered is an error rather than a silent success —
// removing something that was never there is a mistake worth reporting, most
// often a typo the operator would otherwise believe had taken effect.
//
// It does not mutate inv.
func Forget(inv Inventory, name string) (Inventory, error) {
	if _, ok := inv.Boxes[name]; !ok {
		return inv, &UnknownBoxError{Name: name}
	}
	next := clone(inv)
	delete(next.Boxes, name)
	return next, nil
}
