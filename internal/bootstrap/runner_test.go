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

	setupOut    string
	setupErrOut string
	setupErr    error

	copied      bool
	setupRunCmd string
}

func (f *fakeConn) Copy(_ context.Context, _, _ string) error {
	f.copied = true
	return f.copyErr
}

func (f *fakeConn) Run(_ context.Context, cmd string, stdout, stderr io.Writer) error {
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
		if _, err := io.WriteString(stderr, f.setupErrOut); err != nil {
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
	res, err := NewRunner(conn).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3"},
		&out, io.Discard,
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if res.Outcome != OutcomePassed {
		t.Errorf("Outcome = %v, want Passed", res.Outcome)
	}
	if res.Outcome.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", res.Outcome.ExitCode())
	}
	if res.Failure != nil {
		t.Errorf("Failure = %+v, want nil on a clean run", res.Failure)
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
	res, err := NewRunner(conn).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if res.Outcome != OutcomePartial {
		t.Fatalf("Outcome = %v, want Partial", res.Outcome)
	}
	if res.Outcome.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1", res.Outcome.ExitCode())
	}
	if res.Failure == nil {
		t.Fatal("Failure = nil, want a report on a partial run")
	}
}

// TestSetupPhaseFailureReportsRecovery drives a mid-run phase failure and checks
// the SetupResult carries a report that names the failed phase, lists the phases
// already completed, states which door is open, and layers the interpreted
// headline over the raw stderr the box streamed.
func TestSetupPhaseFailureReportsRecovery(t *testing.T) {
	conn := &fakeConn{
		setupOut:    "▶ packages\n✓ packages\n▶ smith-user\n✓ smith-user\n▶ ssh-hardening\n",
		setupErrOut: "ssh-hardening: self-test failed; reverted the hardening drop-in, box left reachable as smith\n",
		setupErr:    fmt.Errorf("phase ssh-hardening: %w", errRemote),
	}
	res, err := NewRunner(conn).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if res.Failure == nil {
		t.Fatal("Failure = nil, want a report on a partial run")
	}
	if res.Failure.FailedPhase != "ssh-hardening" {
		t.Errorf("FailedPhase = %q, want ssh-hardening", res.Failure.FailedPhase)
	}
	if got := strings.Join(res.Failure.CompletedPhases, ","); got != "packages,smith-user" {
		t.Errorf("CompletedPhases = %q, want packages,smith-user", got)
	}
	report := res.Failure.Report()
	for _, want := range []string{"ssh-hardening", "packages", "public SSH", "self-test failed", "resume"} {
		if !strings.Contains(report, want) {
			t.Errorf("Report() missing %q; got:\n%s", want, report)
		}
	}
}

func TestSetupConnectFailureIsConnectFailed(t *testing.T) {
	conn := &fakeConn{copyErr: fmt.Errorf("scp: %w", connection.ErrConnect)}
	res, err := NewRunner(conn).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if res.Outcome != OutcomeConnectFailed {
		t.Fatalf("Outcome = %v, want ConnectFailed", res.Outcome)
	}
	if res.Outcome.ExitCode() != 3 {
		t.Errorf("ExitCode = %d, want 3", res.Outcome.ExitCode())
	}
}

// TestOutcomeExitCode pins the exit-code contract: success 0, partial-but-
// reachable 1, gate rejection 2, connect failure 3.
func TestOutcomeExitCode(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    int
	}{
		{OutcomePassed, 0},
		{OutcomePartial, 1},
		{OutcomeRejected, 2},
		{OutcomeConnectFailed, 3},
	}
	for _, tt := range tests {
		if got := tt.outcome.ExitCode(); got != tt.want {
			t.Errorf("Outcome(%d).ExitCode() = %d, want %d", tt.outcome, got, tt.want)
		}
	}
}

// errRemote is a stand-in for a remote command that ran and failed (as opposed
// to a connection failure).
var errRemote = errors.New("remote command failed")
