package workspace

import (
	"fmt"
	"strings"
)

// Outcome is what converging one unit of work did: the step it was, what it
// changed, and the failure that stopped it if one did.
type Outcome struct {
	// Step is the kind of work the unit was.
	Step Step
	// Summary is what the step did, in the words the operator is told it —
	// empty when the step failed before doing anything.
	Summary string
	// Err is why the step failed, or nil when it converged.
	Err error
}

// line renders the outcome as it is streamed while the stage runs.
func (o Outcome) line() string {
	return fmt.Sprintf("  %-11s %s\n", o.Step, o.detail())
}

// detail is what the outcome says about itself: its failure when it has one,
// and what it did otherwise.
func (o Outcome) detail() string {
	if o.Err != nil {
		return "failed: " + o.Err.Error()
	}
	return o.Summary
}

// Result is what a converge run did, one outcome per unit of work, in the
// order the stage converged them.
type Result struct {
	// Outcomes are the units the run converged, each with what it did.
	Outcomes []Outcome
}

// Failed reports whether any step failed, which is what the caller exits
// non-zero on. Every failure is in the result, not just the first: a run
// converges what it can, so the operator is owed the whole picture rather than
// one line at a time.
func (r Result) Failed() bool {
	for _, o := range r.Outcomes {
		if o.Err != nil {
			return true
		}
	}
	return false
}

// Report renders the final summary. The steps themselves reported as they ran,
// so this counts rather than repeats them — except for the failures, which are
// restated so an operator scrolling back past a long apt log still finds what
// went wrong at the end.
func (r Result) Report() string {
	if len(r.Outcomes) == 0 {
		return "workspace: nothing to converge\n"
	}
	var failed []Outcome
	for _, o := range r.Outcomes {
		if o.Err != nil {
			failed = append(failed, o)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "workspace: %s converged", steps(len(r.Outcomes)-len(failed)))
	if len(failed) == 0 {
		b.WriteString("\n")
		return b.String()
	}
	fmt.Fprintf(&b, ", %d failed\n", len(failed))
	for _, o := range failed {
		b.WriteString(o.line())
	}
	return b.String()
}

// steps counts steps the way the summary reads them.
func steps(n int) string {
	if n == 1 {
		return "1 step"
	}
	return fmt.Sprintf("%d steps", n)
}
