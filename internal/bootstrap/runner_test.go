package bootstrap

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byran/smith/internal/connection"
)

// fakeConn stands in for a real ssh/scp connection. It answers the preflight
// command with canned output and lets each step's error be injected.
type fakeConn struct {
	preflightOut string
	preflightErr error
	probeErr     error
	copyErr      error

	copied bool
}

func (f *fakeConn) Copy(_ context.Context, _, _ string) error {
	f.copied = true
	return f.copyErr
}

func (f *fakeConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	if strings.Contains(cmd, "preflight") {
		if _, err := io.WriteString(stdout, f.preflightOut); err != nil {
			return err
		}
		return f.preflightErr
	}
	return f.probeErr // the reachability probe
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

func TestEmbeddedScriptIsPreflightOnly(t *testing.T) {
	if !strings.Contains(Script, "set -Eeuo pipefail") {
		t.Error("embedded bootstrap.sh must set -Eeuo pipefail")
	}
	if !strings.Contains(Script, "preflight") {
		t.Error("embedded bootstrap.sh must contain the preflight gate")
	}
}
