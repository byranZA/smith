package bootstrap

import (
	"strings"
	"testing"
)

func TestNewFailureReportParsesStream(t *testing.T) {
	stdout := "▶ packages\n✓ packages\n" +
		"▶ smith-user\n✓ smith-user (already-satisfied)\n" +
		"▶ smith-keys\n✓ smith-keys\n" +
		"▶ firewall\n✓ firewall\n" +
		"▶ ssh-hardening\n"
	stderr := "ssh-hardening: self-test failed; reverted the hardening drop-in, box left reachable as smith\n"

	r := newFailureReport(stdout, stderr, publicSSHOpenDoor)

	if r.FailedPhase != "ssh-hardening" {
		t.Errorf("FailedPhase = %q, want ssh-hardening", r.FailedPhase)
	}
	got := strings.Join(r.CompletedPhases, ",")
	if want := "packages,smith-user,smith-keys,firewall"; got != want {
		t.Errorf("CompletedPhases = %q, want %q", got, want)
	}
	if !strings.Contains(r.RawError, "self-test failed") {
		t.Errorf("RawError = %q, want the raw underlying stderr", r.RawError)
	}
	if r.Headline == "" || !strings.Contains(r.Headline, "ssh-hardening") {
		t.Errorf("Headline = %q, want an interpreted line naming the failed phase", r.Headline)
	}
}

// TestFailureReportRendersRecovery checks the rendered report carries every part
// the operator needs: the failed phase, the completed-phase list, which door is
// open, the layered cause (headline + raw error), and the fix/re-run/resumes/
// reachability guidance.
func TestFailureReportRendersRecovery(t *testing.T) {
	r := newFailureReport(
		"▶ packages\n✓ packages\n▶ smith-user\n✓ smith-user\n▶ firewall\n",
		"firewall: ufw refused to enable\n",
		publicSSHOpenDoor,
	)
	out := r.Report()

	for _, want := range []string{
		"firewall",              // failed phase
		"packages",              // a completed phase
		"smith-user",            // another completed phase
		"public SSH",            // which door is open
		"ufw refused to enable", // the raw underlying error
		"re-run",                // fix and re-run guidance
		"resume",                // states the re-run resumes
		"reachable",             // states reachability
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Report() missing %q; got:\n%s", want, out)
		}
	}
}

// TestOpenDoorForRunFollowsTheDoorConnectedOver checks the reported open door is
// derived from the door setup connected over, not hardcoded: a tailscale re-run
// (public SSH already closed) is still reachable over the tailnet, while every
// public-reached run names public SSH on port 22.
func TestOpenDoorForRunFollowsTheDoorConnectedOver(t *testing.T) {
	for _, tt := range []struct {
		name      string
		publicSSH string
		want      string
	}{
		{"public mode leaves public SSH open", "open", publicSSHOpenDoor},
		{"tailscale re-run leaves only the tailnet", "closed", tailnetOpenDoor},
		{"unset defaults to public SSH", "", publicSSHOpenDoor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := openDoorForRun(SetupOptions{PublicSSH: tt.publicSSH}); got != tt.want {
				t.Errorf("openDoorForRun(PublicSSH=%q) = %q, want %q", tt.publicSSH, got, tt.want)
			}
		})
	}
}

// TestFailureReportNamesTailnetDoorOverTailnet covers the tailscale-re-run case
// the hardcoded constant got wrong: when smith connected over the tailnet (public
// SSH already closed), a base-phase failure must report the box as reachable over
// the tailnet, never as public SSH on port 22.
func TestFailureReportNamesTailnetDoorOverTailnet(t *testing.T) {
	over := newFailureReport("▶ packages\n", "packages: apt-get failed\n", tailnetOpenDoor)
	out := over.Report()
	if !strings.Contains(out, "tailnet") {
		t.Errorf("Report() should name the tailnet door; got:\n%s", out)
	}
	if strings.Contains(out, "public SSH") || strings.Contains(out, "port 22") {
		t.Errorf("Report() must not name public SSH when reached over the tailnet; got:\n%s", out)
	}

	pub := newFailureReport("▶ packages\n", "packages: apt-get failed\n", publicSSHOpenDoor)
	if out := pub.Report(); !strings.Contains(out, "public SSH") {
		t.Errorf("Report() should still name public SSH for a public-reached run; got:\n%s", out)
	}
}

// TestNewFailureReportNoPhaseStarted covers a failure before any phase reported
// (e.g. the early marker write): there is no failed phase to name, but the raw
// error must still surface so the operator is not left blind.
func TestNewFailureReportNoPhaseStarted(t *testing.T) {
	r := newFailureReport("", "marker write failed: permission denied\n", publicSSHOpenDoor)

	if r.FailedPhase != "" {
		t.Errorf("FailedPhase = %q, want empty when no phase started", r.FailedPhase)
	}
	if len(r.CompletedPhases) != 0 {
		t.Errorf("CompletedPhases = %v, want none", r.CompletedPhases)
	}
	if !strings.Contains(r.Report(), "marker write failed") {
		t.Errorf("Report() should still surface the raw error; got:\n%s", r.Report())
	}
}

// TestNewFailureReportNamesMissingCapability covers the capability guard: a box
// that clears the OS floor but lacks a capability smith depends on (here systemd)
// fails before any phase mutates, and the report's headline must name the missing
// capability — not the OS, and not a raw "command not found" surfaced from deep
// inside a later phase. The raw underlying error still surfaces for detail.
func TestNewFailureReportNamesMissingCapability(t *testing.T) {
	stderr := "required capability missing: systemd (the systemctl command was not found on the box)\n"

	r := newFailureReport("", stderr, publicSSHOpenDoor)

	if !strings.Contains(r.Headline, "systemd") {
		t.Errorf("Headline = %q, want it to name the missing capability (systemd)", r.Headline)
	}
	if strings.Contains(strings.ToLower(r.Headline), "ubuntu") || strings.Contains(strings.ToLower(r.Headline), "os") {
		t.Errorf("Headline = %q, must name the capability, not the OS", r.Headline)
	}
	if strings.Contains(r.Headline, "systemctl") || strings.Contains(strings.ToLower(r.Headline), "not found") {
		t.Errorf("Headline = %q, must be an interpreted capability name, not a raw command error", r.Headline)
	}
	if !strings.Contains(r.RawError, "systemctl") {
		t.Errorf("RawError = %q, want the raw underlying detail preserved", r.RawError)
	}
	if out := r.Report(); !strings.Contains(out, "systemd") {
		t.Errorf("Report() should name the missing capability; got:\n%s", out)
	}
}
