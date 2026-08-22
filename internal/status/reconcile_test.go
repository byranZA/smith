package status

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/marker"
)

// TestExpectedPhasesTracksBootstrapPhases proves status derives its expected
// phase set from the canonical bootstrap.sh PHASES sequence rather than keeping a
// parallel copy, so the two cannot drift out of order.
func TestExpectedPhasesTracksBootstrapPhases(t *testing.T) {
	if got, want := strings.Join(expectedPhases, ","), strings.Join(bootstrap.Phases, ","); got != want {
		t.Errorf("expectedPhases = %q, want bootstrap.Phases %q", got, want)
	}
}

// known is a determinable probed fact carrying value v.
func known(v string) Fact { return Fact{Value: v, Known: true} }

// unknown is an undeterminable probed fact — the `?` marker.
func unknown() Fact { return Fact{} }

// cleanPublicMarker is a fully-provisioned public-mode marker.
func cleanPublicMarker() marker.Marker {
	return marker.Marker{
		SchemaVersion:   marker.SchemaVersion,
		SmithVersion:    "1.0.0",
		AccessMode:      "public",
		CompletedPhases: []string{"packages", "smith-user", "smith-keys", "firewall", "ssh-hardening", "fail2ban", "auto-updates", "access"},
	}
}

// cleanPublicFacts are live facts that exactly match a clean public box.
func cleanPublicFacts() Facts {
	return Facts{
		SmithUserExists:  true,
		PasswordlessSudo: true,
		UFWActive:        known("active"),
		UFWDefaultDeny:   known("deny"),
		UFWSSHAllow:      known("allow"),
		PermitRootLogin:  known("no"),
		PasswordAuth:     known("no"),
		Fail2banRunning:  known("running"),
		AutoUpdates:      known("enabled"),
	}
}

func TestReconcileMatchingBoxIsClean(t *testing.T) {
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, cleanPublicFacts())
	if r.Verdict != VerdictMatches {
		t.Fatalf("Verdict = %v, want Matches", r.Verdict)
	}
	if r.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", r.ExitCode())
	}
	for _, g := range r.Groups {
		for _, f := range g.Findings {
			if f.Status != StatusMatch {
				t.Errorf("finding %q under %q = %v, want Match", f.Fact, g.Phase, f.Status)
			}
		}
	}
}

func TestReconcileDriftedFactReportsExpectedVersusActual(t *testing.T) {
	facts := cleanPublicFacts()
	facts.PasswordAuth = known("yes") // sshd changed to PasswordAuthentication yes
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts)
	if r.Verdict != VerdictDrifted {
		t.Fatalf("Verdict = %v, want Drifted", r.Verdict)
	}
	if r.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1", r.ExitCode())
	}
	f := findFact(t, r, "ssh-hardening", "PasswordAuthentication")
	if f.Status != StatusDrift {
		t.Errorf("Status = %v, want Drift", f.Status)
	}
	if f.Expected != "no" || f.Actual != "yes" {
		t.Errorf("finding = expected %q actual %q, want expected no actual yes", f.Expected, f.Actual)
	}
	out := r.String()
	for _, want := range []string{"ssh-hardening", "PasswordAuthentication", "no", "yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() missing %q; got:\n%s", want, out)
		}
	}
}

func TestReconcileUndeterminableFactFoldsIntoDrifted(t *testing.T) {
	facts := cleanPublicFacts()
	facts.Fail2banRunning = unknown()
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts)
	if r.Verdict != VerdictDrifted {
		t.Fatalf("Verdict = %v, want Drifted (an unknown fact must not silently match)", r.Verdict)
	}
	f := findFact(t, r, "fail2ban", "fail2ban service")
	if f.Status != StatusUnknown {
		t.Errorf("Status = %v, want Unknown", f.Status)
	}
}

func TestReconcileLostSudoIsDriftAndBlocksDependentProbes(t *testing.T) {
	facts := cleanPublicFacts()
	facts.PasswordlessSudo = false // smith user lost passwordless sudo
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts)
	if r.Verdict != VerdictDrifted {
		t.Fatalf("Verdict = %v, want Drifted", r.Verdict)
	}
	if r.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1", r.ExitCode())
	}
	sudo := findFact(t, r, "smith-user", "passwordless sudo")
	if sudo.Status != StatusDrift {
		t.Errorf("passwordless sudo Status = %v, want Drift", sudo.Status)
	}
	// A probe that needed root is marked unverifiable, not matched or drifted.
	hardening := findFact(t, r, "ssh-hardening", "PermitRootLogin")
	if hardening.Status != StatusUnverifiable {
		t.Errorf("PermitRootLogin Status = %v, want Unverifiable when sudo is lost", hardening.Status)
	}
	if !strings.Contains(strings.ToLower(r.String()), "sudo") {
		t.Errorf("String() should explain lost sudo; got:\n%s", r.String())
	}
}

func TestReconcileNeverBootstrapped(t *testing.T) {
	r := Reconcile(marker.Marker{}, marker.SkewNone, false, Facts{})
	if r.Verdict != VerdictNeverBootstrapped {
		t.Fatalf("Verdict = %v, want NeverBootstrapped", r.Verdict)
	}
	if r.ExitCode() != 2 {
		t.Errorf("ExitCode = %d, want 2", r.ExitCode())
	}
}

func TestReconcilePartialIsKeptSeparateFromDrift(t *testing.T) {
	m := cleanPublicMarker()
	m.CompletedPhases = []string{"packages", "smith-user", "smith-keys", "firewall"} // stopped partway
	r := Reconcile(m, marker.SkewNone, true, cleanPublicFacts())
	if r.Verdict != VerdictPartial {
		t.Fatalf("Verdict = %v, want Partial", r.Verdict)
	}
	if r.ExitCode() != 2 {
		t.Errorf("ExitCode = %d, want 2", r.ExitCode())
	}
	out := r.String()
	if !strings.Contains(out, "ssh-hardening") {
		t.Errorf("String() should list a missing phase; got:\n%s", out)
	}
}

func TestReconcileTailscaleAccessGroup(t *testing.T) {
	m := cleanPublicMarker()
	m.AccessMode = "tailscale"
	facts := cleanPublicFacts()
	facts.UFWSSHAllow = known("deny") // public 22 closed in tailscale mode
	facts.TailscaleRunning = known("running")
	facts.TailnetReach = known("reachable")
	r := Reconcile(m, marker.SkewNone, true, facts)
	if r.Verdict != VerdictMatches {
		t.Fatalf("Verdict = %v, want Matches; report:\n%s", r.Verdict, r.String())
	}
	// public 22 open would be a drift in tailscale mode.
	drifted := facts
	drifted.UFWSSHAllow = known("allow")
	if got := Reconcile(m, marker.SkewNone, true, drifted).Verdict; got != VerdictDrifted {
		t.Errorf("open public 22 in tailscale mode: Verdict = %v, want Drifted", got)
	}
	// the access group's reach probe must be present.
	findFact(t, r, "access", "tailnet ssh reach")
}

func TestReconcileSchemaSkewNewerNotesUpgrade(t *testing.T) {
	r := Reconcile(cleanPublicMarker(), marker.SkewNewer, true, cleanPublicFacts())
	if !strings.Contains(strings.ToLower(r.String()), "upgrade smith") {
		t.Errorf("String() should carry the upgrade-smith note for a newer schema; got:\n%s", r.String())
	}
}

func TestVerdictExitCodes(t *testing.T) {
	tests := []struct {
		verdict Verdict
		want    int
	}{
		{VerdictMatches, 0},
		{VerdictDrifted, 1},
		{VerdictNeverBootstrapped, 2},
		{VerdictPartial, 2},
		{VerdictUnreachable, 3},
	}
	for _, tt := range tests {
		if got := tt.verdict.ExitCode(); got != tt.want {
			t.Errorf("Verdict(%v).ExitCode() = %d, want %d", tt.verdict, got, tt.want)
		}
	}
}

func TestNonCleanVerdictClosesWithReRunRemedy(t *testing.T) {
	facts := cleanPublicFacts()
	facts.PasswordAuth = known("yes")
	out := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts).String()
	if !strings.Contains(out, "setup") {
		t.Errorf("a drifted report should point at re-running setup to converge; got:\n%s", out)
	}
}

// findFact returns the finding for factName under phase, failing the test if it
// is absent.
func findFact(t *testing.T, r Report, phase, factName string) Finding {
	t.Helper()
	for _, g := range r.Groups {
		if g.Phase != phase {
			continue
		}
		for _, f := range g.Findings {
			if f.Fact == factName {
				return f
			}
		}
	}
	t.Fatalf("no finding %q under phase %q in report:\n%s", factName, phase, r.String())
	return Finding{}
}

// TestReconcileReportsAPreviousSchemaMarkersFacts proves a box set up before
// the marker schema advanced still gets a full status report, with the older
// schema noted rather than the report withheld: the fields the older schema
// never had are simply absent, and every fact it does carry is still reconciled.
func TestReconcileReportsAPreviousSchemaMarkersFacts(t *testing.T) {
	m := cleanPublicMarker()
	m.SchemaVersion = marker.SchemaVersion - 1

	r := Reconcile(m, marker.SkewOlder, true, cleanPublicFacts())

	if r.Verdict != VerdictMatches {
		t.Fatalf("Verdict = %v, want Matches: an older marker's facts still reconcile", r.Verdict)
	}
	if len(r.Groups) == 0 {
		t.Error("report carries no findings, want the facts an older marker still describes")
	}
	if !strings.Contains(r.String(), "older smith schema") {
		t.Errorf("report = %q, want it to note the older marker schema", r.String())
	}
}
