package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
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

// fakeBox fakes a box at the Conn seam. It tracks which login owns each file
// and directory and, as a sticky /tmp would, refuses a copy over a file another
// login owns or into a directory it does not own. mktemp hands out a fresh
// directory per call, rm -rf deletes one, and the probe subcommand only runs
// against a script that is actually on the box.
type fakeBox struct {
	probeOut    string
	probeErr    error
	copyErr     error
	rmErr       error
	unreachable bool

	owner   map[string]string // remote path -> owning login
	dirs    int
	probed  []string // script paths the probe ran against
	removed []string // directories rm -rf deleted
}

func newFakeBox(probeOut string) *fakeBox {
	return &fakeBox{probeOut: probeOut, owner: map[string]string{}}
}

// named resolves a shell argument to the path on the box it names, reading it
// as a remote shell would: only a quoted argument names a path holding
// whitespace or shell metacharacters.
func (b *fakeBox) named(arg string) (string, bool) {
	for p := range b.owner {
		if arg == connection.ShellArg(p) {
			return p, true
		}
	}
	return "", false
}

// login returns a connection to the box as user.
func (b *fakeBox) login(user string) *boxConn { return &boxConn{box: b, user: user} }

// leftovers lists the paths still on the box under dir.
func (b *fakeBox) leftovers(dir string) []string {
	var paths []string
	for p := range b.owner {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			paths = append(paths, p)
		}
	}
	return paths
}

type boxConn struct {
	box  *fakeBox
	user string
}

func (c *boxConn) Copy(_ context.Context, _, remotePath string) error {
	b := c.box
	if b.unreachable {
		return connection.ErrConnect
	}
	if b.copyErr != nil {
		return b.copyErr
	}
	if owner, ok := b.owner[remotePath]; ok && owner != c.user {
		return fmt.Errorf("scp %s: %s owned by %s: exit status 1", c.user, remotePath, owner)
	}
	if dir := path.Dir(remotePath); dir != "/tmp" && b.owner[dir] != c.user {
		return fmt.Errorf("scp %s: %s not writable: exit status 1", c.user, dir)
	}
	b.owner[remotePath] = c.user
	return nil
}

func (c *boxConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	b := c.box
	if b.unreachable {
		return connection.ErrConnect
	}
	switch {
	case cmd == "true":
		return nil
	case strings.HasPrefix(cmd, "mktemp -d"):
		b.dirs++
		dir := fmt.Sprintf("/tmp/smith's dir; $HOME.%08d", b.dirs)
		b.owner[dir] = c.user
		_, err := fmt.Fprintln(stdout, dir)
		return err
	case strings.HasPrefix(cmd, "rm -rf -- "):
		dir, ok := b.named(strings.TrimPrefix(cmd, "rm -rf -- "))
		if !ok {
			return nil // rm -rf of a path that is not there succeeds
		}
		if b.rmErr != nil {
			return b.rmErr
		}
		for _, p := range b.leftovers(dir) {
			delete(b.owner, p)
		}
		b.removed = append(b.removed, dir)
		return nil
	case strings.HasPrefix(cmd, "bash ") && strings.HasSuffix(cmd, " probe"):
		script, ok := b.named(strings.TrimSuffix(strings.TrimPrefix(cmd, "bash "), " probe"))
		if !ok {
			return fmt.Errorf("bash: %s: no such file", cmd)
		}
		b.probed = append(b.probed, script)
		if _, err := io.WriteString(stdout, b.probeOut); err != nil {
			return err
		}
		return b.probeErr
	}
	return fmt.Errorf("unexpected command %q", cmd)
}

// fakeAdmin fakes the admin-side tailnet reach probe.
type fakeAdmin struct{ probeErr error }

func (a *fakeAdmin) Status(context.Context) (tailscale.AdminStatus, error) {
	return tailscale.AdminStatus{OnTailnet: true}, nil
}
func (a *fakeAdmin) Probe(context.Context, string) error { return a.probeErr }

func TestGatherUnreachable(t *testing.T) {
	box := newFakeBox("")
	box.unreachable = true
	g, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Reachable {
		t.Errorf("Reachable = true, want false on a connect failure")
	}
}

func TestGatherProbesTailnetReachInTailscaleMode(t *testing.T) {
	box := newFakeBox(`marker-begin
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
`)
	g, err := NewProber(box.login("smith"), &fakeAdmin{probeErr: nil}).Gather(context.Background())
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
	box := newFakeBox(`marker-begin
{"schema_version":1,"access_mode":"tailscale","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
tailscale-running=running
tailnet-ip=100.64.0.5
`)
	g, err := NewProber(box.login("smith"), &fakeAdmin{probeErr: tailscale.ErrProbeDenied}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if g.Facts.TailnetReach != known("denied") {
		t.Errorf("TailnetReach = %+v, want known denied", g.Facts.TailnetReach)
	}
}

// publicProbe is probe output for a clean public-mode box.
const publicProbe = `marker-begin
{"schema_version":1,"access_mode":"public","completed_phases":["packages"]}
marker-end
smith-user-exists=yes
passwordless-sudo=yes
`

// staleScriptPath is the fixed path v0.2.0-rc.1 shipped bootstrap.sh to, which a
// root setup left behind root-owned.
const staleScriptPath = "/tmp/smith-bootstrap.sh"

func TestGatherWorksOnABoxSetUpAsRoot(t *testing.T) {
	box := newFakeBox(publicProbe)
	// Setup ran as root and left its script behind at the old fixed path, as a
	// v0.2.0-rc.1 box does.
	if err := bootstrap.ShipTo(context.Background(), box.login("root"), bootstrap.Script, staleScriptPath); err != nil {
		t.Fatalf("ship as root: %v", err)
	}

	g, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() as smith error = %v", err)
	}
	if !g.Reachable || !g.MarkerPresent {
		t.Errorf("Reachable/MarkerPresent = %v/%v, want true/true", g.Reachable, g.MarkerPresent)
	}
	if owner := box.owner[staleScriptPath]; owner != "root" {
		t.Errorf("stale %s owned by %q, want it left untouched as root's", staleScriptPath, owner)
	}
}

func TestGatherProbesAgainstAFreshPathEachRun(t *testing.T) {
	box := newFakeBox(publicProbe)
	prober := NewProber(box.login("smith"), &fakeAdmin{})
	for range 2 {
		if _, err := prober.Gather(context.Background()); err != nil {
			t.Fatalf("Gather() error = %v", err)
		}
	}
	if len(box.probed) != 2 || box.probed[0] == box.probed[1] {
		t.Fatalf("probed %q, want two runs against different paths", box.probed)
	}
	for _, p := range box.probed {
		if path.Dir(p) == "/tmp" {
			t.Errorf("probed %q, a fixed name under /tmp, want a private directory", p)
		}
	}
}

func TestGatherRemovesTheShippedScript(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeBox)
	}{
		{"success", func(*fakeBox) {}},
		{"probe failure", func(b *fakeBox) { b.probeErr = errors.New("probe: exit status 1") }},
		{"box unreachable mid-probe", func(b *fakeBox) { b.probeErr = connection.ErrConnect }},
		{"malformed marker", func(b *fakeBox) { b.probeOut = "marker-begin\n{not json\nmarker-end\n" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := newFakeBox(publicProbe)
			tt.setup(box)
			// The outcome varies by case; only what is left on the box matters here.
			g, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background())
			if len(box.probed) != 1 {
				t.Fatalf("probed %q (Gather = %+v, %v), want one probe run", box.probed, g, err)
			}
			if left := box.leftovers(path.Dir(box.probed[0])); len(left) != 0 {
				t.Errorf("left %q on the box, want the shipped directory removed", left)
			}
		})
	}
}

func TestGatherIgnoresAFailedRemove(t *testing.T) {
	box := newFakeBox(publicProbe)
	box.rmErr = errors.New("rm: permission denied")
	g, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background())
	if err != nil {
		t.Fatalf("Gather() error = %v, want the failed remove ignored", err)
	}
	if !g.Reachable || !g.MarkerPresent {
		t.Errorf("Reachable/MarkerPresent = %v/%v, want true/true", g.Reachable, g.MarkerPresent)
	}
}

func TestGatherClassifiesAFailedCopy(t *testing.T) {
	t.Run("connect failure is unreachable", func(t *testing.T) {
		box := newFakeBox(publicProbe)
		box.copyErr = connection.ErrConnect
		g, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background())
		if err != nil || g.Reachable {
			t.Errorf("Gather() = %+v, %v, want unreachable and no error", g, err)
		}
		if left := box.leftovers("/tmp/smith.00000001"); len(left) != 0 {
			t.Errorf("left %q on the box, want the directory it made removed", left)
		}
	})
	t.Run("other failure is an error", func(t *testing.T) {
		box := newFakeBox(publicProbe)
		box.copyErr = errors.New("scp: exit status 1")
		if _, err := NewProber(box.login("smith"), &fakeAdmin{}).Gather(context.Background()); err == nil {
			t.Error("Gather() error = nil, want the copy failure")
		}
		if left := box.leftovers("/tmp/smith.00000001"); len(left) != 0 {
			t.Errorf("left %q on the box, want the directory it made removed", left)
		}
	})
}
