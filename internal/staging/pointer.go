package staging

import (
	"fmt"

	"github.com/byranZA/smith/internal/marker"
)

// PointerError reports a run that supplied no blueprint against a box whose
// marker records one. It names the blueprint the box was built from, because
// that is the thing the operator has to pass to re-run.
type PointerError struct {
	// Blueprint is the blueprint pointer the box's marker records.
	Blueprint string
}

// Error implements error.
func (e *PointerError) Error() string {
	return fmt.Sprintf(
		"this box was built from the blueprint %q: re-run with --blueprint %s, "+
			"or the staged config would come to describe a box that no longer matches it",
		e.Blueprint, e.Blueprint)
}

// Pointer applies the blueprint-pointer rule to the marker a run read off the
// box: a run that names no blueprint against a box whose marker records one is
// refused, naming the blueprint the box was built from.
//
// This is the only rule under which the pointer cannot come to describe a box
// that no longer matches it. The alternatives are worse in both directions: a
// blueprint-less run that converged the staged tree would strip the box of its
// workspace, and one that left the tree alone would leave the pointer pointing
// at a blueprint the box is no longer being maintained against.
//
// A run that names a blueprint is not this rule's business — the operator has
// said what the box is to become. A box whose marker records no blueprint, or
// that carries no marker at all and so is passed the zero marker, has no
// pointer: the run proceeds and stages nothing, which is the flag-only path.
//
// It is a decision on a value: it reaches no box, so a refusal leaves the
// staged document and the staged placements exactly as they were.
func Pointer(m marker.Marker, blueprintName string) error {
	if blueprintName != "" || m.Blueprint == "" {
		return nil
	}
	return &PointerError{Blueprint: m.Blueprint}
}
