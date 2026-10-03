package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/shipped"
)

// reachConn is the bootstrap login's connection: it answers the reachability
// probe with err.
type reachConn struct {
	err error
}

func (c reachConn) Run(context.Context, string, io.Writer, io.Writer) error {
	return c.err
}

// scriptReplying returns a shipped bootstrap.sh that answers sub with reply.
func scriptReplying(sub string, reply shipped.Reply) *shipped.Fake {
	return &shipped.Fake{Replies: map[string]shipped.Reply{sub: reply}}
}

// lastCall is the last subcommand script was asked to run.
func lastCall(t *testing.T, script *shipped.Fake) shipped.Call {
	t.Helper()
	if len(script.Calls) == 0 {
		t.Fatal("no subcommand ran")
	}
	return script.Calls[len(script.Calls)-1]
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
	script := scriptReplying("preflight", shipped.Reply{Stdout: preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04")})
	res, err := NewRunner(reachConn{}, script).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomePassed {
		t.Errorf("Outcome = %v, want Passed (reason %q)", res.Outcome, res.Reason)
	}
	if res.Outcome.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", res.Outcome.ExitCode())
	}
}

func TestPreflightRunsAnOrdinarySubcommand(t *testing.T) {
	script := scriptReplying("preflight", shipped.Reply{Stdout: preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04")})
	if _, err := NewRunner(reachConn{}, script).Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if call := lastCall(t, script); call.Sub != "preflight" || call.Final {
		t.Errorf("ran %+v, want preflight as an ordinary subcommand so setup can reuse the copy", call)
	}
}

func TestPreflightRejectsMissingPasswordlessSudo(t *testing.T) {
	script := scriptReplying("preflight", shipped.Reply{Stdout: preflightOutput("none", "ubuntu", "24.04.1 LTS (Noble)", "24.04")})
	res, err := NewRunner(reachConn{}, script).Preflight(context.Background())
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
	script := scriptReplying("preflight", shipped.Reply{Stdout: preflightOutput("root", "debian", "12 (Bookworm)", "12")})
	res, err := NewRunner(reachConn{}, script).Preflight(context.Background())
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
	script := &shipped.Fake{}
	res, err := NewRunner(reachConn{err: fmt.Errorf("dial: %w", connection.ErrConnect)}, script).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomeConnectFailed {
		t.Fatalf("Outcome = %v, want ConnectFailed", res.Outcome)
	}
	if res.Outcome.ExitCode() != 3 {
		t.Errorf("ExitCode = %d, want 3", res.Outcome.ExitCode())
	}
	if len(script.Calls) != 0 {
		t.Errorf("ran %+v, want no subcommand when the box is unreachable", script.Calls)
	}
}

func TestPreflightShipConnectFailureIsConnectFailed(t *testing.T) {
	script := scriptReplying("preflight", shipped.Reply{Err: fmt.Errorf("ship bootstrap.sh: scp: %w", connection.ErrConnect)})
	res, err := NewRunner(reachConn{}, script).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if res.Outcome != OutcomeConnectFailed {
		t.Errorf("Outcome = %v, want ConnectFailed", res.Outcome)
	}
}

func TestReportMentionsReason(t *testing.T) {
	script := scriptReplying("preflight", shipped.Reply{Stdout: preflightOutput("none", "ubuntu", "24.04 LTS", "24.04")})
	res, err := NewRunner(reachConn{}, script).Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
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
	script := scriptReplying("setup", shipped.Reply{Stdout: "▶ packages\n✓ packages\n"})
	var out bytes.Buffer
	res, err := NewRunner(reachConn{}, script).Setup(
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
	if !strings.Contains(out.String(), "▶ packages") {
		t.Errorf("Setup did not stream phase progress to stdout: %q", out.String())
	}
}

func TestSetupRunsAsTheFinalSubcommand(t *testing.T) {
	script := &shipped.Fake{}
	if _, err := NewRunner(reachConn{}, script).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3"},
		io.Discard, io.Discard,
	); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if call := lastCall(t, script); call.Sub != "setup" || !call.Final {
		t.Errorf("ran %+v, want setup as the final subcommand, which removes its own copy", call)
	}
}

func TestSetupPassesItsOptions(t *testing.T) {
	tests := []struct {
		name string
		opts SetupOptions
		want []string
	}{
		{
			name: "an unnamed box from no blueprint",
			opts: SetupOptions{AccessMode: "public", SmithVersion: "1.2.3"},
			want: []string{"--access", "public", "--smith-version", "1.2.3", "--public-ssh", "open"},
		},
		{
			name: "a named box",
			opts: SetupOptions{AccessMode: "public", SmithVersion: "1.2.3", BoxName: "dev"},
			want: []string{"--access", "public", "--smith-version", "1.2.3", "--public-ssh", "open", "--name", "dev"},
		},
		{
			name: "a box from a blueprint",
			opts: SetupOptions{AccessMode: "public", SmithVersion: "1.2.3", Blueprint: "acme"},
			want: []string{"--access", "public", "--smith-version", "1.2.3", "--public-ssh", "open", "--blueprint", "acme"},
		},
		{
			name: "tailscale with public SSH closed",
			opts: SetupOptions{AccessMode: "tailscale", SmithVersion: "1.2.3", PublicSSH: "closed"},
			want: []string{"--access", "tailscale", "--smith-version", "1.2.3", "--public-ssh", "closed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := &shipped.Fake{}
			if _, err := NewRunner(reachConn{}, script).Setup(context.Background(), tt.opts, io.Discard, io.Discard); err != nil {
				t.Fatalf("Setup() error = %v", err)
			}
			if got := lastCall(t, script).Args; !slices.Equal(got, tt.want) {
				t.Errorf("setup args = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetupPhaseFailureIsPartial(t *testing.T) {
	script := scriptReplying("setup", shipped.Reply{Err: fmt.Errorf("phase packages: %w", errRemote)})
	res, err := NewRunner(reachConn{}, script).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
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

func TestSetupPhaseFailureReportsRecovery(t *testing.T) {
	script := scriptReplying("setup", shipped.Reply{
		Stdout: "▶ packages\n✓ packages\n▶ smith-user\n✓ smith-user\n▶ ssh-hardening\n",
		Stderr: "ssh-hardening: self-test failed; reverted the hardening drop-in, box left reachable as smith\n",
		Err:    fmt.Errorf("phase ssh-hardening: %w", errRemote),
	})
	res, err := NewRunner(reachConn{}, script).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
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
	script := scriptReplying("setup", shipped.Reply{Err: fmt.Errorf("ship bootstrap.sh: scp: %w", connection.ErrConnect)})
	res, err := NewRunner(reachConn{}, script).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
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

func TestSetupShipFailureIsNotAPartialBox(t *testing.T) {
	notShipped := fmt.Errorf("ship bootstrap.sh: %w: copy: No space left on device", shipped.ErrNotShipped)
	script := scriptReplying("setup", shipped.Reply{Err: notShipped})
	res, err := NewRunner(reachConn{}, script).Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if !errors.Is(err, shipped.ErrNotShipped) {
		t.Errorf("Setup() error = %v, want it to report that nothing ran on the box", err)
	}
	if res.Failure != nil {
		t.Errorf("Failure = %+v, want no phase-failure report when nothing ran", res.Failure)
	}
}

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

// errRemote stands in for a remote command that ran and failed.
var errRemote = errors.New("remote command failed")
