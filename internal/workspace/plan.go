package workspace

import "github.com/byranZA/smith/internal/blueprint"

// Plan derives the ordered units of work from the blueprint staged on the box.
// It is pure: it reads nothing, writes nothing, runs no command and reaches no
// box, so the whole ordering rule can be read and tested without an apt.
//
// A step the blueprint declares nothing for plans nothing, so a blueprint with
// no packages does not report a packages step that had nothing to do.
func Plan(b blueprint.Blueprint) []Unit {
	var plan []Unit
	for _, step := range Order() {
		plan = append(plan, planStep(step, b)...)
	}
	return plan
}

// planStep is what one step of the order plans from the blueprint. The steps
// that are not yet built plan nothing and are named in Order regardless, so the
// slice that builds one adds a case here and inherits its place in the
// sequence rather than deciding it again.
func planStep(step Step, b blueprint.Blueprint) []Unit {
	if step != Packages || len(b.Packages) == 0 {
		return nil
	}
	return []Unit{{Step: Packages, Packages: append([]string(nil), b.Packages...)}}
}
