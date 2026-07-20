package tailscale

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byran/smith/internal/connection"
)

// probeExec is a process-launch boundary fake for the admin probe tests: it
// records the launched command and returns a fakeExit with the configured code.
type probeExec struct {
	name     string
	args     []string
	exitCode int
}

func (p *probeExec) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	p.name = name
	p.args = args
	if p.exitCode != 0 {
		return fakeExit{code: p.exitCode}
	}
	return nil
}

// fakeExit models a process error carrying an ssh exit status.
type fakeExit struct{ code int }

func (e fakeExit) Error() string { return "exit status" }
func (e fakeExit) ExitCode() int { return e.code }

func TestProbeConnectFailureIsNotProbeDenied(t *testing.T) {
	fake := &probeExec{exitCode: 255} // ssh's connect-class exit status
	a := NewAdmin(fake)

	err := a.Probe(context.Background(), "100.64.0.1")
	if err == nil {
		t.Fatal("Probe() error = nil, want a connect failure")
	}
	if errors.Is(err, ErrProbeDenied) {
		t.Errorf("Probe() error = %v, a connect failure must not be attributed to the missing ssh ACL prerequisite", err)
	}
	if !errors.Is(err, connection.ErrConnect) {
		t.Errorf("Probe() error = %v, want wrapping connection.ErrConnect", err)
	}
	if fake.name != "ssh" || !strings.Contains(strings.Join(fake.args, " "), "smith@100.64.0.1") {
		t.Errorf("probe launched %q %q, want ssh to smith@100.64.0.1 via the connection boundary", fake.name, fake.args)
	}
}

func TestProbeRanAndDeniedIsProbeDenied(t *testing.T) {
	fake := &probeExec{exitCode: 1} // remote command ran and was refused
	a := NewAdmin(fake)

	err := a.Probe(context.Background(), "100.64.0.1")
	if !errors.Is(err, ErrProbeDenied) {
		t.Errorf("Probe() error = %v, want wrapping ErrProbeDenied for a ran-and-denied probe", err)
	}
}

func TestParseAdminStatusRunningWithIdentity(t *testing.T) {
	data := []byte(`{
	  "BackendState": "Running",
	  "Self": { "UserID": 12345 },
	  "User": {
	    "12345": { "LoginName": "op@example.com" },
	    "999": { "LoginName": "someone@else.com" }
	  }
	}`)
	got, err := parseAdminStatus(data)
	if err != nil {
		t.Fatalf("parseAdminStatus() error = %v", err)
	}
	if !got.OnTailnet {
		t.Error("OnTailnet = false, want true for BackendState Running")
	}
	if got.Identity != "op@example.com" {
		t.Errorf("Identity = %q, want op@example.com resolved via the Self UserID", got.Identity)
	}
}

func TestParseAdminStatusStoppedIsOffTailnet(t *testing.T) {
	data := []byte(`{ "BackendState": "Stopped", "Self": { "UserID": 1 }, "User": {} }`)
	got, err := parseAdminStatus(data)
	if err != nil {
		t.Fatalf("parseAdminStatus() error = %v", err)
	}
	if got.OnTailnet {
		t.Error("OnTailnet = true, want false when BackendState is not Running")
	}
}
