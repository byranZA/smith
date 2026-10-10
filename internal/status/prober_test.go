package status

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/memory"
	"github.com/byranZA/smith/internal/shipped"
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

// fakeAdmin fakes the admin-side tailnet reach probe.
type fakeAdmin struct{ probeErr error }

func (a *fakeAdmin) Status(context.Context) (tailscale.AdminStatus, error) {
	return tailscale.AdminStatus{OnTailnet: true}, nil
}
func (a *fakeAdmin) Probe(context.Context, string) error { return a.probeErr }

// probing returns a shipped-script fake whose probe subcommand prints out.
func probing(out string) *shipped.Fake {
	return &shipped.Fake{Replies: map[string]shipped.Reply{"probe": {Stdout: out}}}
}

// failing returns a shipped-script fake whose probe subcommand fails with err.
func failing(err error) *shipped.Fake {
	return &shipped.Fake{Replies: map[string]shipped.Reply{"probe": {Err: err}}}
}

// publicProbe is probe output for a clean public-mode box.
const publicProbe = `marker-begin
{"schema_version":1,"access_mode":"public","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
`

func TestGatherUnreachable(t *testing.T) {
	script := probing(publicProbe)
	g, err := NewProber(&scriptedConn{err: connection.ErrConnect}, script, &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Reachable {
		t.Errorf("Reachable = true, want false on a connect failure")
	}
	if len(script.Calls) != 0 {
		t.Errorf("ran %+v, want nothing run on an unreachable box", script.Calls)
	}
}

func TestGatherRunsTheProbeSubcommand(t *testing.T) {
	script := probing(publicProbe)
	g, err := NewProber(&scriptedConn{}, script, &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if len(script.Calls) != 1 || script.Calls[0].Sub != "probe" || len(script.Calls[0].Args) != 0 {
		t.Errorf("ran %+v, want the probe subcommand once with no arguments", script.Calls)
	}
	if !g.Reachable || !g.MarkerPresent {
		t.Errorf("Reachable/MarkerPresent = %v/%v, want true/true", g.Reachable, g.MarkerPresent)
	}
}

func TestGatherReportsMemoryAndSwap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		lines      string
		wantMemory memory.Total
		wantSwap   memory.Swap
	}{
		{"reported figures", "mem-total-kb=4026532\nswap-total-kb=2097148\n", memory.FromKiB(4026532), memory.SwapFromKiB(2097148)},
		{"zero swap", "mem-total-kb=4026532\nswap-total-kb=0\n", memory.FromKiB(4026532), memory.SwapFromKiB(0)},
		{"missing lines", "", memory.Total{}, memory.Swap{}},
		{"empty values", "mem-total-kb=\nswap-total-kb=\n", memory.Total{}, memory.Swap{}},
		{"non-numeric values", "mem-total-kb=?\nswap-total-kb=lots\n", memory.Total{}, memory.Swap{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, err := NewProber(&scriptedConn{}, probing(publicProbe+tt.lines), &fakeAdmin{}).Gather(context.Background())
			if err != nil {
				t.Fatalf("Gather() error = %v", err)
			}
			if g.Facts.Memory != tt.wantMemory || g.Facts.Swap != tt.wantSwap {
				t.Errorf("Gather() memory/swap = %v/%v, want %v/%v", g.Facts.Memory, g.Facts.Swap, tt.wantMemory, tt.wantSwap)
			}
		})
	}
}

func TestGatherProbesTailnetReachInTailscaleMode(t *testing.T) {
	script := probing(`marker-begin
{"schema_version":1,"smith_version":"1.0.0","access_mode":"tailscale","completed_phases":["swap","packages","smith-user","smith-keys","firewall","ssh-hardening","fail2ban","auto-updates","access"]}
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
`)
	g, err := NewProber(&scriptedConn{}, script, &fakeAdmin{probeErr: nil}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if !g.Reachable || !g.MarkerPresent {
		t.Fatalf("Reachable/MarkerPresent = %v/%v, want true/true", g.Reachable, g.MarkerPresent)
	}
	if g.Facts.TailnetReach != known("reachable") {
		t.Errorf("TailnetReach = %+v, want known reachable", g.Facts.TailnetReach)
	}
	if v := Reconcile(g.Marker, g.Skew, g.MarkerPresent, g.Facts).Verdict; v != VerdictMatches {
		t.Errorf("Verdict = %v, want Matches", v)
	}
}

func TestGatherTailnetReachDenied(t *testing.T) {
	script := probing(`marker-begin
{"schema_version":1,"access_mode":"tailscale","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
tailscale-running=running
tailnet-ip=100.64.0.5
`)
	g, err := NewProber(&scriptedConn{}, script, &fakeAdmin{probeErr: tailscale.ErrProbeDenied}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Facts.TailnetReach != known("denied") {
		t.Errorf("TailnetReach = %+v, want known denied", g.Facts.TailnetReach)
	}
}

func TestGatherClosesTheShippedScript(t *testing.T) {
	tests := []struct {
		name   string
		script *shipped.Fake
	}{
		{"success", probing(publicProbe)},
		{"probe failure", failing(errors.New("probe: exit status 1"))},
		{"box unreachable mid-probe", failing(connection.ErrConnect)},
		{"script not shipped", failing(shipped.ErrNotShipped)},
		{"malformed marker", probing("marker-begin\n{not json\nmarker-end\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewProber(&scriptedConn{}, tt.script, &fakeAdmin{}).Gather(context.Background())
			if !tt.script.Closed {
				t.Errorf("Gather() = %+v, %v left the shipped script open, want it closed", g, err)
			}
		})
	}
}

func TestGatherClassifiesAFailedProbe(t *testing.T) {
	t.Run("connect failure is unreachable", func(t *testing.T) {
		g, err := NewProber(&scriptedConn{}, failing(connection.ErrConnect), &fakeAdmin{}).Gather(context.Background())
		if err != nil || g.Reachable {
			t.Errorf("Gather() = %+v, %v, want unreachable and no error", g, err)
		}
	})
	t.Run("ship failure is a not-shipped error", func(t *testing.T) {
		_, err := NewProber(&scriptedConn{}, failing(shipped.ErrNotShipped), &fakeAdmin{}).Gather(context.Background())
		if !errors.Is(err, shipped.ErrNotShipped) {
			t.Errorf("Gather() error = %v, want it to wrap ErrNotShipped", err)
		}
	})
	t.Run("run failure is an error", func(t *testing.T) {
		_, err := NewProber(&scriptedConn{}, failing(errors.New("probe: exit status 1")), &fakeAdmin{}).Gather(context.Background())
		if err == nil || errors.Is(err, shipped.ErrNotShipped) {
			t.Errorf("Gather() error = %v, want the run failure", err)
		}
	})
}

func TestGatherKeepsTheClassifiedConnectFailure(t *testing.T) {
	refused := fmt.Errorf("ssh root@box: %w", connection.ErrAuthRefused)
	tests := []struct {
		name   string
		conn   *scriptedConn
		script *shipped.Fake
	}{
		{"reach check", &scriptedConn{err: refused}, probing(publicProbe)},
		{"shipped probe", &scriptedConn{}, failing(fmt.Errorf("run bootstrap.sh probe: %w", refused))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewProber(tt.conn, tt.script, &fakeAdmin{}).Gather(context.Background())
			if err != nil || g.Reachable || !errors.Is(g.ConnectErr, connection.ErrAuthRefused) {
				t.Errorf("Gather() = %+v, %v, want unreachable carrying ErrAuthRefused", g, err)
			}
		})
	}
}
