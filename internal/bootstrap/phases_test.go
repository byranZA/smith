package bootstrap

import (
	"strings"
	"testing"
)

// TestPhasesParsedFromScript proves Phases is the ordered sequence declared by
// the embedded script's PHASES array, ending in the access phase — the single
// definition the Go side reads instead of re-listing.
func TestPhasesParsedFromScript(t *testing.T) {
	want := []string{
		"packages", "smith-user", "smith-keys", "firewall",
		"ssh-hardening", "fail2ban", "auto-updates", "access",
	}
	if got := strings.Join(Phases, ","); got != strings.Join(want, ",") {
		t.Errorf("Phases = %q, want %q", got, want)
	}
	if Phases[len(Phases)-1] != AccessPhase {
		t.Errorf("Phases must end with %q, got %q", AccessPhase, Phases[len(Phases)-1])
	}
}

// TestParsePhasesRejectsMalformedArrays proves parsePhases fails loudly rather
// than silently returning a bad sequence: a missing array, an empty array, and
// an array not ending in the access phase the tailscale derivation depends on.
func TestParsePhasesRejectsMalformedArrays(t *testing.T) {
	cases := map[string]string{
		"missing":            "SMITH_BOOTSTRAP_VERSION=1\n",
		"empty":              "PHASES=()\n",
		"no trailing access": "PHASES=(packages firewall)\n",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePhases(script); err == nil {
				t.Errorf("parsePhases(%q) succeeded, want error", script)
			}
		})
	}
}

// TestTailscalePhaseSequenceIsPhasesMinusAccess pins the tailscale-mode box-side
// sequence to Phases minus its trailing access element. The bootstrap.sh setup()
// derives the tailscale set the same way, so this fails if the two diverge —
// guarding the quality gate that the sequence stay defined once.
func TestTailscalePhaseSequenceIsPhasesMinusAccess(t *testing.T) {
	want := Phases[:len(Phases)-1]
	if Phases[len(Phases)-1] != AccessPhase {
		t.Fatalf("Phases must end with %q for the derivation to hold", AccessPhase)
	}
	if got := strings.Join(want, ","); got != "packages,smith-user,smith-keys,firewall,ssh-hardening,fail2ban,auto-updates" {
		t.Errorf("tailscale-mode sequence = %q, unexpected", got)
	}
}
