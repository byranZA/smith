package bootstrap

import (
	"fmt"
	"strings"
)

// phaseStartPrefix and phaseDonePrefix are the markers bootstrap.sh streams as
// each phase begins and succeeds. The Go side parses them back out to learn which
// phase failed and which completed, without the box reporting that state itself.
const (
	phaseStartPrefix = "▶ "
	phaseDonePrefix  = "✓ "
	satisfiedSuffix  = " (already-satisfied)"
	// publicSSHOpenDoor and tailnetOpenDoor name the reach left open after a
	// partial base-layer run — the door setup connected over, which a base-layer
	// failure never closes. Public mode (and a tailscale first run, still reached
	// over public SSH) leaves public port 22 open; a tailscale re-run reaches the
	// box over the tailnet with public SSH already closed.
	publicSSHOpenDoor = "public SSH on port 22 (log in as smith on the box's public IP)"
	tailnetOpenDoor   = "the tailnet (log in as smith over the box's tailnet name)"
	// publicSSHClosedTarget is the SetupOptions.PublicSSH value meaning public
	// port 22 is kept closed — the firewall target a tailscale re-run carries. It
	// mirrors tailscale.PublicSSHClosed.String(); SetupOptions.PublicSSH stays a
	// plain string so the bootstrap runner need not depend on the access layer.
	publicSSHClosedTarget = "closed"
)

// missingCapabilityPrefix is the sentinel bootstrap.sh's setup capability guard
// prints on stderr when the box clears the OS floor but lacks a capability smith
// depends on (apt, systemd). The Go side lifts the named capability out of it into
// an interpreted headline, so a missing capability reads as a named failure rather
// than a raw "command not found" surfaced from deep inside a later phase. Keep it
// in sync with CAPABILITY_MISSING_PREFIX in bootstrap.sh.
const missingCapabilityPrefix = "required capability missing: "

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

// openDoorForRun names the reach left open after a partial base-layer run,
// derived from the door setup connected over rather than hardcoded: the tailnet
// when public SSH was already closed on a tailscale re-run, and public SSH on
// port 22 otherwise. A base-layer failure never closes the door it connected on.
func openDoorForRun(opts SetupOptions) string {
	if opts.PublicSSH == publicSSHClosedTarget {
		return tailnetOpenDoor
	}
	return publicSSHOpenDoor
}

// newFailureReport reconstructs a FailureReport from a failed setup run's live
// streams — bootstrap.sh's per-phase stdout (which phases started and succeeded)
// and its stderr (the raw underlying error) — attributing the still-open reach to
// openDoor, the door setup connected over.
func newFailureReport(stdout, stderr, openDoor string) FailureReport {
	completed, failed := parseSetupStream(stdout)

	headline := "setup failed before any phase completed"
	if failed != "" {
		headline = fmt.Sprintf("the %q phase failed", failed)
	}
	// A missing capability the box lacks is named explicitly, overriding the
	// phase-based headline, so the operator sees the capability rather than a raw
	// command error.
	if capability := missingCapability(stderr); capability != "" {
		headline = "required capability missing: " + capability
	}

	return FailureReport{
		FailedPhase:     failed,
		CompletedPhases: completed,
		OpenDoor:        openDoor,
		Headline:        headline,
		RawError:        strings.TrimSpace(stderr),
	}
}

// missingCapability scans a failed setup run's stderr for bootstrap.sh's
// capability-guard sentinel and returns the named capability, or "" when the
// failure was not a missing capability. It reads the capability token that
// follows the sentinel prefix, stopping at the first space so a trailing
// parenthetical detail (the probing command) is left out of the name.
func missingCapability(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), missingCapabilityPrefix)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
		return name
	}
	return ""
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

	fmt.Fprintf(&b, "  The box was left partial but is still reachable over %s; no door was closed.\n", r.OpenDoor)
	b.WriteString("  Fix the cause and re-run setup — the re-run resumes: the completed phases above are no-ops.\n")

	return b.String()
}
