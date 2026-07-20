package bootstrap

import (
	"fmt"
	"regexp"
	"strings"
)

// phasesLine matches the PHASES=(...) array declaration in bootstrap.sh, the
// single box-side definition of the ordered mutating phase sequence.
var phasesLine = regexp.MustCompile(`(?m)^PHASES=\(([^)]*)\)`)

// Phases is the ordered mutating phase sequence bootstrap.sh runs, parsed once
// from the embedded script's PHASES array so the Go side and the box share a
// single definition instead of re-declaring the list. The names are the marker's
// completed_phases values; status derives its expected phase set from this rather
// than keeping a parallel copy that could drift out of order.
var Phases = mustParsePhases(Script)

// AccessPhase is the terminal phase that completes the sequence in both access
// modes (public via a box-side no-op, tailscale via the admin-driven
// close-public-ssh). It is always the last element of Phases.
const AccessPhase = "access"

// mustParsePhases parses the PHASES array out of the embedded script and panics
// on failure. The script is compiled into the binary, so a parse failure is a
// build-time programming error — a malformed or missing PHASES line — never a
// runtime condition, and a panic surfaces it at package initialization.
func mustParsePhases(script string) []string {
	phases, err := parsePhases(script)
	if err != nil {
		panic(fmt.Sprintf("bootstrap: parse PHASES from embedded script: %v", err))
	}
	return phases
}

// parsePhases extracts the ordered phase names from a bootstrap.sh source's
// PHASES=(...) array. It errors if the array is absent or empty, or if it does
// not end with the AccessPhase the tailscale-mode derivation relies on.
func parsePhases(script string) ([]string, error) {
	m := phasesLine.FindStringSubmatch(script)
	if m == nil {
		return nil, fmt.Errorf("no PHASES=(...) array found")
	}
	phases := strings.Fields(m[1])
	if len(phases) == 0 {
		return nil, fmt.Errorf("PHASES array is empty")
	}
	if last := phases[len(phases)-1]; last != AccessPhase {
		return nil, fmt.Errorf("PHASES must end with %q, found %q", AccessPhase, last)
	}
	return phases, nil
}
