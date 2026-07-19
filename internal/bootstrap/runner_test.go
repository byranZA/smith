package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byran/smith/internal/connection"
)

// fakeConn stands in for a real ssh/scp connection. It answers the preflight
// and setup commands with canned output and lets each step's error be injected.
type fakeConn struct {
	preflightOut string
	preflightErr error
	probeErr     error
	copyErr      error

	setupOut string
	setupErr error

	copied      bool
	setupRunCmd string
}

func (f *fakeConn) Copy(_ context.Context, _, _ string) error {
	f.copied = true
	return f.copyErr
}

func (f *fakeConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	switch {
	case strings.Contains(cmd, "preflight"):
		if _, err := io.WriteString(stdout, f.preflightOut); err != nil {
			return err
		}
		return f.preflightErr
	case strings.Contains(cmd, "setup"):
		f.setupRunCmd = cmd
		if _, err := io.WriteString(stdout, f.setupOut); err != nil {
			return err
		}
		return f.setupErr
	default:
		return f.probeErr // the reachability probe
	}
}

func preflightOutput(privilege, id, version, versionID string) string {
	return fmt.Sprintf(`smith-preflight version=1
privilege=%s
os-release-begin
NAME="Ubuntu"
ID=%s
VERSION="%s"
VERSION_ID="%s"
os-release-end
`, privilege, id, version, versionID)
}

func TestPreflightPasses(t *testing.T) {
	conn := &fakeConn{preflightOut: preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04")}
	res, err := NewRunner(conn).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomePassed {
		t.Errorf("Outcome = %v, want Passed (reason %q)", res.Outcome, res.Reason)
	}
	if res.Outcome.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", res.Outcome.ExitCode())
	}
	if !conn.copied {
		t.Error("expected the script to be shipped to the box")
	}
}

func TestPreflightRejectsMissingPasswordlessSudo(t *testing.T) {
	conn := &fakeConn{preflightOut: preflightOutput("none", "ubuntu", "24.04.1 LTS (Noble)", "24.04")}
	res, err := NewRunner(conn).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomeRejected {
		t.Fatalf("Outcome = %v, want Rejected", res.Outcome)
	}
	if res.Outcome.ExitCode() != 2 {
		t.Errorf("ExitCode = %d, want 2", res.Outcome.ExitCode())
	}
	if !strings.Contains(strings.ToLower(res.Reason), "sudo") {
		t.Errorf("Reason = %q, want it to mention passwordless sudo", res.Reason)
	}
}

func TestPreflightRejectsUnsupportedOS(t *testing.T) {
	conn := &fakeConn{preflightOut: preflightOutput("root", "debian", "12 (Bookworm)", "12")}
	res, err := NewRunner(conn).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomeRejected {
		t.Fatalf("Outcome = %v, want Rejected", res.Outcome)
	}
	if res.Outcome.ExitCode() != 2 {
		t.Errorf("ExitCode = %d, want 2", res.Outcome.ExitCode())
	}
	if !strings.Contains(strings.ToLower(res.Reason), "ubuntu") {
		t.Errorf("Reason = %q, want it to name the OS problem", res.Reason)
	}
}

func TestPreflightConnectFailure(t *testing.T) {
	conn := &fakeConn{probeErr: fmt.Errorf("dial: %w", connection.ErrConnect)}
	res, err := NewRunner(conn).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomeConnectFailed {
		t.Fatalf("Outcome = %v, want ConnectFailed", res.Outcome)
	}
	if res.Outcome.ExitCode() != 3 {
		t.Errorf("ExitCode = %d, want 3", res.Outcome.ExitCode())
	}
	if conn.copied {
		t.Error("must not ship the script when the box is unreachable")
	}
}

func TestReportMentionsReason(t *testing.T) {
	conn := &fakeConn{preflightOut: preflightOutput("none", "ubuntu", "24.04 LTS", "24.04")}
	res, _ := NewRunner(conn).Preflight(context.Background())
	if !strings.Contains(res.Report(), res.Reason) {
		t.Errorf("Report() = %q, want it to contain the reason %q", res.Report(), res.Reason)
	}
}

func TestEmbeddedScriptHasPhaseFramework(t *testing.T) {
	for _, want := range []string{"set -Eeuo pipefail", "preflight", "setup", "packages", "bootstrap.json", "apt-get"} {
		if !strings.Contains(Script, want) {
			t.Errorf("embedded bootstrap.sh must contain %q", want)
		}
	}
}

func TestSetupStreamsAndPasses(t *testing.T) {
	conn := &fakeConn{setupOut: "▶ packages\n✓ packages\n"}
	var out bytes.Buffer
	outcome, err := NewRunner(conn).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3"},
		&out, io.Discard,
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if outcome != OutcomePassed {
		t.Errorf("Outcome = %v, want Passed", outcome)
	}
	if outcome.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", outcome.ExitCode())
	}
	if !conn.copied {
		t.Error("Setup must ship the script to the box")
	}
	if !strings.Contains(out.String(), "▶ packages") {
		t.Errorf("Setup did not stream phase progress to stdout: %q", out.String())
	}
	if !strings.Contains(conn.setupRunCmd, "--access 'public'") || !strings.Contains(conn.setupRunCmd, "--smith-version '1.2.3'") {
		t.Errorf("setup command = %q, want it to pass access mode and smith version", conn.setupRunCmd)
	}
}

func TestSetupPhaseFailureIsPartial(t *testing.T) {
	conn := &fakeConn{setupErr: fmt.Errorf("phase packages: %w", errRemote)}
	outcome, err := NewRunner(conn).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if outcome != OutcomePartial {
		t.Fatalf("Outcome = %v, want Partial", outcome)
	}
	if outcome.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1", outcome.ExitCode())
	}
}

func TestSetupConnectFailureIsConnectFailed(t *testing.T) {
	conn := &fakeConn{copyErr: fmt.Errorf("scp: %w", connection.ErrConnect)}
	outcome, err := NewRunner(conn).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if outcome != OutcomeConnectFailed {
		t.Fatalf("Outcome = %v, want ConnectFailed", outcome)
	}
	if outcome.ExitCode() != 3 {
		t.Errorf("ExitCode = %d, want 3", outcome.ExitCode())
	}
}

// errRemote is a stand-in for a remote command that ran and failed (as opposed
// to a connection failure).
var errRemote = errors.New("remote command failed")
