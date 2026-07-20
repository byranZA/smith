package tailscale

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeBox records the box-side steps the access sequence drives, and lets each
// be made to fail, so a test can assert the verify order and that public SSH is
// only ever closed after a successful probe.
type fakeBox struct {
	currentIP  string
	currentErr error

	enrollIP  string
	enrollErr error

	closeErr error

	enrolled bool
	closed   bool
	gotHost  string
	gotKey   string
}

func (b *fakeBox) CurrentIP(_ context.Context) (string, error) {
	if b.currentErr != nil {
		return "", b.currentErr
	}
	return b.currentIP, nil
}

func (b *fakeBox) Enroll(_ context.Context, opts EnrollOptions) (string, error) {
	b.enrolled = true
	b.gotHost = opts.Host
	b.gotKey = opts.AuthKey
	if b.enrollErr != nil {
		return "", b.enrollErr
	}
	return b.enrollIP, nil
}

func (b *fakeBox) ClosePublicSSH(_ context.Context) error {
	b.closed = true
	return b.closeErr
}

// fakeAdmin fakes the admin machine's own tailscale/ssh surface.
type fakeAdmin struct {
	status    AdminStatus
	statusErr error
	probeErr  error

	probedIP string
	probed   bool
}

func (a *fakeAdmin) Status(_ context.Context) (AdminStatus, error) {
	if a.statusErr != nil {
		return AdminStatus{}, a.statusErr
	}
	return a.status, nil
}

func (a *fakeAdmin) Probe(_ context.Context, tailnetIP string) error {
	a.probed = true
	a.probedIP = tailnetIP
	return a.probeErr
}

func establishOpts() EstablishOptions {
	return EstablishOptions{
		Host:       "box.example.com",
		AcquireKey: func() (string, error) { return "tskey-abc123", nil },
	}
}

func TestEstablishClosesPublicSSHOnlyAfterProbe(t *testing.T) {
	box := &fakeBox{enrollIP: "100.101.102.103"}
	admin := &fakeAdmin{}
	res, err := NewAccess(box, admin).Establish(context.Background(), establishOpts())
	if err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	if !box.enrolled {
		t.Error("box was never enrolled")
	}
	if !admin.probed {
		t.Error("tailnet probe never ran")
	}
	if admin.probedIP != "100.101.102.103" {
		t.Errorf("probed IP = %q, want the enrolled tailnet IP", admin.probedIP)
	}
	if !box.closed {
		t.Error("public SSH was never closed after a successful probe")
	}
	if !res.PublicSSHClosed {
		t.Error("Result.PublicSSHClosed = false, want true")
	}
	if res.TailnetIP != "100.101.102.103" {
		t.Errorf("Result.TailnetIP = %q, want the enrolled IP", res.TailnetIP)
	}
	if box.gotHost != "box.example.com" || box.gotKey != "tskey-abc123" {
		t.Errorf("enroll got host=%q key=%q, want the establish options passed through", box.gotHost, box.gotKey)
	}
}

func TestEstablishAlreadyRunningAndReachableIsANoOp(t *testing.T) {
	box := &fakeBox{currentIP: "100.101.102.103"}
	admin := &fakeAdmin{}
	acquired := false
	opts := EstablishOptions{
		Host: "box.example.com",
		AcquireKey: func() (string, error) {
			acquired = true
			return "tskey-fresh", nil
		},
	}
	res, err := NewAccess(box, admin).Establish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	if box.enrolled {
		t.Error("re-enrolled a box already Running and reachable; enrollment is not check-before-change")
	}
	if acquired {
		t.Error("acquired a fresh auth key for an already-satisfied box; a re-run must not need a new key")
	}
	if !admin.probed {
		t.Error("skipped the tailnet probe; already-satisfied must be proven, not assumed")
	}
	if admin.probedIP != "100.101.102.103" {
		t.Errorf("probed IP = %q, want the box's current tailnet IP", admin.probedIP)
	}
	if box.closed {
		t.Error("closed public SSH again on an already-satisfied re-run")
	}
	if !res.AlreadySatisfied {
		t.Error("Result.AlreadySatisfied = false, want true for an already-reachable box")
	}
	if res.PublicSSHClosed {
		t.Error("Result.PublicSSHClosed = true, but this run closed nothing")
	}
	if res.TailnetIP != "100.101.102.103" {
		t.Errorf("Result.TailnetIP = %q, want the box's current tailnet IP", res.TailnetIP)
	}
	if res.ReRunHost != "smith-box.example.com" {
		t.Errorf("Result.ReRunHost = %q, want the tailnet node name", res.ReRunHost)
	}
}

func TestEstablishEnrollsWhenNotYetRunning(t *testing.T) {
	box := &fakeBox{currentIP: "", enrollIP: "100.64.0.9"}
	admin := &fakeAdmin{}
	res, err := NewAccess(box, admin).Establish(context.Background(), establishOpts())
	if err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	if !box.enrolled {
		t.Error("an un-enrolled box was not enrolled")
	}
	if box.gotKey != "tskey-abc123" {
		t.Errorf("enroll got key %q, want the freshly acquired key", box.gotKey)
	}
	if !box.closed {
		t.Error("public SSH was never closed after a fresh enroll + probe")
	}
	if res.AlreadySatisfied {
		t.Error("Result.AlreadySatisfied = true, want false for a fresh enroll")
	}
	if !res.PublicSSHClosed {
		t.Error("Result.PublicSSHClosed = false, want true after a fresh enroll")
	}
}

func TestEstablishReadingBoxStatusFailsBeforeEnrolling(t *testing.T) {
	box := &fakeBox{currentErr: errors.New("ssh boom")}
	admin := &fakeAdmin{}
	_, err := NewAccess(box, admin).Establish(context.Background(), establishOpts())
	if err == nil {
		t.Fatal("Establish() error = nil, want the box-status read error surfaced")
	}
	if box.enrolled {
		t.Error("enrolled despite being unable to read the box's tailscale status")
	}
}

func TestEstablishMissingTagOwnersAttributedAtEnrollment(t *testing.T) {
	box := &fakeBox{enrollErr: ErrEnrollNotRunning}
	admin := &fakeAdmin{}
	_, err := NewAccess(box, admin).Establish(context.Background(), establishOpts())
	if err == nil {
		t.Fatal("Establish() error = nil, want a tagOwners attribution error")
	}
	if !errors.Is(err, ErrEnrollNotRunning) {
		t.Errorf("error = %v, want it to wrap ErrEnrollNotRunning", err)
	}
	if !strings.Contains(err.Error(), "tagOwners") {
		t.Errorf("error = %q, want it to attribute the missing tagOwners prerequisite", err)
	}
	if admin.probed {
		t.Error("probe ran after a failed enrollment; verify order violated")
	}
	if box.closed {
		t.Error("public SSH was closed despite a failed enrollment")
	}
}

func TestEstablishMissingSSHRuleAttributedAtProbe(t *testing.T) {
	box := &fakeBox{enrollIP: "100.64.0.5"}
	admin := &fakeAdmin{probeErr: ErrProbeDenied}
	_, err := NewAccess(box, admin).Establish(context.Background(), establishOpts())
	if err == nil {
		t.Fatal("Establish() error = nil, want an ssh-ACL attribution error")
	}
	if !errors.Is(err, ErrProbeDenied) {
		t.Errorf("error = %v, want it to wrap ErrProbeDenied", err)
	}
	if !strings.Contains(err.Error(), "ssh") {
		t.Errorf("error = %q, want it to attribute the missing ssh ACL prerequisite", err)
	}
	if !box.enrolled {
		t.Error("box should have enrolled before the probe")
	}
	if box.closed {
		t.Error("public SSH was closed despite a denied probe; the door must stay open")
	}
}

func TestCheckAdminOnTailnetRefusesWhenNotOnTailnet(t *testing.T) {
	admin := &fakeAdmin{status: AdminStatus{OnTailnet: false}}
	err := CheckAdminOnTailnet(context.Background(), admin)
	if err == nil {
		t.Fatal("CheckAdminOnTailnet() error = nil, want a refusal")
	}
	if !errors.Is(err, ErrNotOnTailnet) {
		t.Errorf("error = %v, want it to wrap ErrNotOnTailnet", err)
	}
}

func TestCheckAdminOnTailnetPassesWhenOnTailnet(t *testing.T) {
	admin := &fakeAdmin{status: AdminStatus{OnTailnet: true, Identity: "op@example.com"}}
	if err := CheckAdminOnTailnet(context.Background(), admin); err != nil {
		t.Errorf("CheckAdminOnTailnet() error = %v, want nil when the admin machine is on the tailnet", err)
	}
}

func TestPrereqsArePersonalizedAndNameBothOneTimeSetups(t *testing.T) {
	out := Prereqs("op@example.com", "box.example.com")
	for _, want := range []string{"op@example.com", "tag:smith", "tagOwners", "ssh", "accept"} {
		if !strings.Contains(out, want) {
			t.Errorf("Prereqs() missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"check"`) || strings.Contains(out, "action: check") {
		t.Errorf("Prereqs() should recommend accept, not check:\n%s", out)
	}
}

func TestPublicSSHTargetIsAccessAware(t *testing.T) {
	tests := []struct {
		name        string
		access      string
		overTailnet bool
		want        PublicSSH
	}{
		{"public mode keeps 22 open", "public", false, PublicSSHOpen},
		{"tailscale fresh over public keeps 22 open until proven", "tailscale", false, PublicSSHOpen},
		{"tailscale re-run over the tailnet keeps 22 closed", "tailscale", true, PublicSSHClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PublicSSHTarget(tt.access, tt.overTailnet); got != tt.want {
				t.Errorf("PublicSSHTarget(%q, %v) = %v, want %v", tt.access, tt.overTailnet, got, tt.want)
			}
		})
	}
}

func TestConnectedOverTailnetClassifiesSSHConnectionSource(t *testing.T) {
	tests := []struct {
		name          string
		sshConnection string
		want          bool
	}{
		{"tailnet CGNAT source", "100.101.102.103 54321 100.64.0.1 22", true},
		{"tailnet lower edge", "100.64.0.1 1 100.64.0.2 22", true},
		{"public source", "203.0.113.7 54321 203.0.113.9 22", false},
		{"just below the CGNAT range", "100.63.255.255 1 10.0.0.1 22", false},
		{"just above the CGNAT range", "100.128.0.0 1 10.0.0.1 22", false},
		{"private LAN source", "192.168.1.5 1 192.168.1.1 22", false},
		{"empty (no ssh session)", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConnectedOverTailnet(tt.sshConnection); got != tt.want {
				t.Errorf("ConnectedOverTailnet(%q) = %v, want %v", tt.sshConnection, got, tt.want)
			}
		})
	}
}
