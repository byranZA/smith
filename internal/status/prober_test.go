package status

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/tailscale"
)

func TestParseProbeReadsMarkerAndFacts(t *testing.T) {
	out := `marker-begin
{
  "schema_version": 1,
  "access_mode": "public",
  "completed_phases": ["packages"]
}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
ufw-active=active
ufw-default-deny=deny
ufw-ssh-allow=allow
permit-root-login=no
password-authentication=no
fail2ban=running
auto-updates=enabled
`
	facts, markerRaw, present, ip := parseProbe(out)
	if !present {
		t.Fatal("markerPresent = false, want true")
	}
	if !strings.Contains(string(markerRaw), `"access_mode": "public"`) {
		t.Errorf("markerRaw = %q, want the marker JSON", markerRaw)
	}
	if ip != "" {
		t.Errorf("tailnetIP = %q, want empty in public mode", ip)
	}
	if !facts.SmithUserExists || !facts.PasswordlessSudo {
		t.Errorf("user/sudo = %v/%v, want true/true", facts.SmithUserExists, facts.PasswordlessSudo)
	}
	if facts.PermitRootLogin != known("no") || facts.PasswordAuth != known("no") {
		t.Errorf("sshd facts = %+v/%+v, want known no", facts.PermitRootLogin, facts.PasswordAuth)
	}
	if facts.Fail2banRunning != known("running") || facts.AutoUpdates != known("enabled") {
		t.Errorf("service facts = %+v/%+v", facts.Fail2banRunning, facts.AutoUpdates)
	}
}

func TestParseProbeMarksUndeterminableFacts(t *testing.T) {
	out := `marker-begin
{"schema_version":1,"access_mode":"public","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
ufw-active=active
ufw-default-deny=deny
ufw-ssh-allow=allow
permit-root-login=?
password-authentication=no
fail2ban=running
auto-updates=enabled
`
	facts, _, _, _ := parseProbe(out)
	if facts.PermitRootLogin.Known {
		t.Errorf("PermitRootLogin.Known = true, want false for a `?` value")
	}
}

func TestParseProbeNoMarker(t *testing.T) {
	out := `marker-begin
marker-end
smith-user-exists=no
passwordless-sudo=no
`
	_, _, present, _ := parseProbe(out)
	if present {
		t.Errorf("markerPresent = true, want false when the marker block is empty")
	}
}

func TestParseProbeTailnetIP(t *testing.T) {
	out := `marker-begin
{"schema_version":1,"access_mode":"tailscale","completed_phases":[]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
tailscale-running=running
tailnet-ip=100.64.0.5
`
	facts, _, _, ip := parseProbe(out)
	if ip != "100.64.0.5" {
		t.Errorf("tailnetIP = %q, want 100.64.0.5", ip)
	}
	if facts.TailscaleRunning != known("running") {
		t.Errorf("TailscaleRunning = %+v, want known running", facts.TailscaleRunning)
	}
}

// probeConn fakes the box-side connection for Gather: it answers the reachability
// probe and the probe subcommand, with injectable errors.
type probeConn struct {
	probeOut string
	runErr   error
	copyErr  error
}

func (c *probeConn) Copy(_ context.Context, _, _ string) error { return c.copyErr }

func (c *probeConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	if strings.Contains(cmd, "probe") {
		if _, err := io.WriteString(stdout, c.probeOut); err != nil {
			return err
		}
	}
	return c.runErr
}

// fakeAdmin fakes the admin-side tailnet reach probe.
type fakeAdmin struct{ probeErr error }

func (a *fakeAdmin) Status(context.Context) (tailscale.AdminStatus, error) {
	return tailscale.AdminStatus{OnTailnet: true}, nil
}
func (a *fakeAdmin) Probe(context.Context, string) error { return a.probeErr }

func TestGatherUnreachable(t *testing.T) {
	conn := &probeConn{runErr: errors.New("dial: " + connection.ErrConnect.Error())}
	conn.runErr = connection.ErrConnect
	g, err := NewProber(conn, &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Reachable {
		t.Errorf("Reachable = true, want false on a connect failure")
	}
}

func TestGatherProbesTailnetReachInTailscaleMode(t *testing.T) {
	conn := &probeConn{probeOut: `marker-begin
{"schema_version":1,"smith_version":"1.0.0","access_mode":"tailscale","completed_phases":["packages","smith-user","smith-keys","firewall","ssh-hardening","fail2ban","auto-updates","access"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
ufw-active=active
ufw-default-deny=deny
ufw-ssh-allow=deny
permit-root-login=no
password-authentication=no
fail2ban=running
auto-updates=enabled
tailscale-running=running
tailnet-ip=100.64.0.5
`}
	g, err := NewProber(conn, &fakeAdmin{probeErr: nil}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if !g.Reachable || !g.MarkerPresent {
		t.Fatalf("Reachable/MarkerPresent = %v/%v, want true/true", g.Reachable, g.MarkerPresent)
	}
	if g.Facts.TailnetReach != known("reachable") {
		t.Errorf("TailnetReach = %+v, want known reachable", g.Facts.TailnetReach)
	}
	// The whole box is clean, so it should reconcile to matches.
	if v := Reconcile(g.Marker, g.Skew, g.MarkerPresent, g.Facts).Verdict; v != VerdictMatches {
		t.Errorf("Verdict = %v, want Matches", v)
	}
}

func TestGatherTailnetReachDenied(t *testing.T) {
	conn := &probeConn{probeOut: `marker-begin
{"schema_version":1,"access_mode":"tailscale","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
tailscale-running=running
tailnet-ip=100.64.0.5
`}
	g, err := NewProber(conn, &fakeAdmin{probeErr: tailscale.ErrProbeDenied}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Facts.TailnetReach != known("denied") {
		t.Errorf("TailnetReach = %+v, want known denied", g.Facts.TailnetReach)
	}
}
