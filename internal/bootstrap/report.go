package bootstrap

import (
	"fmt"
	"strings"
)

// phaseStartPrefix and phaseDonePrefix are the markers bootstrap.sh streams as
// each phase begins and succeeds. The Go side parses them back out to learn which
// phase failed and which completed, without the box reporting that state itself.
const (
	phaseStartPrefix    = "▶ "
	phaseDonePrefix     = "✓ "
	satisfiedSuffix     = " (already-satisfied)"
	publicSSHOpenDoor   = "public SSH on port 22 (log in as smith over the door setup connected on)"
	recoveryReachedLine = "The box was left partial but is still reachable over " + publicSSHOpenDoor + "; no door was closed."
)

// FailureReport describes a mid-run phase failure so the operator can recover:
// the phase that failed, the phases that completed before it, which door is
// still open, and a layered cause — an interpreted headline over the raw
// underlying error the box reported.
type FailureReport struct {
	// FailedPhase is the phase that failed, in the marker's phase vocabulary.
	// Empty when setup failed before any phase reported (e.g. the early marker write).
	FailedPhase string
	// CompletedPhases are the phases that succeeded before the failure, in order.
	CompletedPhases []string
	// OpenDoor names the reach that is still open after the partial run — the door
	// setup connected over, which a base-layer failure never closes.
	OpenDoor string
	// Headline is the interpreted one-line summary of what failed — the top layer
	// of the cause, sitting over RawError.
	Headline string
	// RawError is the raw underlying error the box streamed on stderr — the bottom
	// layer of the cause.
	RawError string
}

// newFailureReport reconstructs a FailureReport from a failed setup run's live
// streams: bootstrap.sh's per-phase stdout (which phases started and succeeded)
// and its stderr (the raw underlying error).
func newFailureReport(stdout, stderr string) FailureReport {
	completed, failed := parseSetupStream(stdout)

	headline := "setup failed before any phase completed"
	if failed != "" {
		headline = fmt.Sprintf("the %q phase failed", failed)
	}

	return FailureReport{
		FailedPhase:     failed,
		CompletedPhases: completed,
		OpenDoor:        publicSSHOpenDoor,
		Headline:        headline,
		RawError:        strings.TrimSpace(stderr),
	}
}

// parseSetupStream reads bootstrap.sh's live phase stream and returns the phases
// that completed, in order, and the phase that had started but never reported
// success — the one that failed. When the run stopped between phases, failed is
// empty.
func parseSetupStream(stdout string) (completed []string, failed string) {
	var started string
	for _, line := range strings.Split(stdout, "\n") {
		switch {
		case strings.HasPrefix(line, phaseStartPrefix):
			started = strings.TrimSpace(strings.TrimPrefix(line, phaseStartPrefix))
		case strings.HasPrefix(line, phaseDonePrefix):
			name := strings.TrimPrefix(line, phaseDonePrefix)
			name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), satisfiedSuffix))
			completed = append(completed, name)
			if name == started {
				started = ""
			}
		}
	}
	return completed, started
}

// Report renders the failure report the operator sees: the failed phase, the
// completed-phase list, which door is still open, the layered cause (interpreted
// headline over the raw error), and the fix-and-re-run guidance that states the
// re-run resumes and that the box stays reachable.
func (r FailureReport) Report() string {
	var b strings.Builder

	if r.FailedPhase != "" {
		fmt.Fprintf(&b, "✗ setup failed at the %q phase\n\n", r.FailedPhase)
	} else {
		b.WriteString("✗ setup failed\n\n")
	}

	fmt.Fprintf(&b, "  cause: %s\n", r.Headline)
	if r.RawError != "" {
		b.WriteString("  underlying error:\n")
		for _, line := range strings.Split(r.RawError, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	b.WriteString("\n")

	if len(r.CompletedPhases) > 0 {
		fmt.Fprintf(&b, "  completed phases: %s\n", strings.Join(r.CompletedPhases, ", "))
	} else {
		b.WriteString("  completed phases: none\n")
	}
	fmt.Fprintf(&b, "  still reachable over: %s\n\n", r.OpenDoor)

	b.WriteString("  " + recoveryReachedLine + "\n")
	b.WriteString("  Fix the cause and re-run setup — the re-run resumes: the completed phases above are no-ops.\n")

	return b.String()
}
