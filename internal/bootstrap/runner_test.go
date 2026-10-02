package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// fakeConn stands in for a real ssh/scp connection. It answers the preflight
// and setup commands with canned output, hands out a fresh directory per
// mktemp, records where each copy landed and which commands ran, and lets each
// step's error be injected.
type fakeConn struct {
	preflightOut string
	preflightErr error
	probeErr     error
	copyErr      error

	setupOut    string
	setupErrOut string
	setupErr    error

	copied      bool
	copiedTo    []string
	setupRunCmd string
	dirs        int
	runs        []string
}

func (f *fakeConn) Copy(_ context.Context, _, remotePath string) error {
	f.copied = true
	f.copiedTo = append(f.copiedTo, remotePath)
	return f.copyErr
}

func (f *fakeConn) Run(_ context.Context, cmd string, stdout, stderr io.Writer) error {
	f.runs = append(f.runs, cmd)
	switch {
	case strings.HasPrefix(cmd, "mktemp"):
		f.dirs++
		_, err := fmt.Fprintln(stdout, shippedDir(f.dirs))
		return err
	case strings.HasPrefix(cmd, "rm "):
		return nil
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

// shippedDir is the nth private directory the fake box's mktemp hands out. It
// holds whitespace, an apostrophe and shell metacharacters, as a box's TMPDIR
// may, so a script argument only names the shipped file when it is quoted.
func shippedDir(n int) string {
	return fmt.Sprintf("/tmp/smith's dir; $HOME.%08d", n)
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

// TestSetupPassesTheBlueprintPointer proves setup hands the box the blueprint
// the operator built it from, so the marker records the pointer. Without it a
// box could never say what kind of box it is.
func TestSetupPassesTheBlueprintPointer(t *testing.T) {
	conn := &fakeConn{}
	if _, err := NewRunner(conn).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3", Blueprint: "acme"},
		io.Discard, io.Discard,
	); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if !strings.Contains(conn.setupRunCmd, "--blueprint 'acme'") {
		t.Errorf("setup command = %q, want it to pass the blueprint pointer", conn.setupRunCmd)
	}
}

// TestSetupPassesTheBoxName proves setup hands the box the name the operator
// gave it, so the marker records it and the box can say what it is called.
func TestSetupPassesTheBoxName(t *testing.T) {
	conn := &fakeConn{}
	if _, err := NewRunner(conn).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3", BoxName: "dev"},
		io.Discard, io.Discard,
	); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if !strings.Contains(conn.setupRunCmd, "--name 'dev'") {
		t.Errorf("setup command = %q, want it to pass the box name", conn.setupRunCmd)
	}
}

// TestSetupOmitsAnUnnamedBoxAndAnAbsentBlueprint proves a run that names neither
// a box name nor a blueprint passes neither flag, so the box records no key for
// what the run did not say — rather than recording each as the empty string.
func TestSetupOmitsAnUnnamedBoxAndAnAbsentBlueprint(t *testing.T) {
	conn := &fakeConn{}
	if _, err := NewRunner(conn).Setup(
		context.Background(),
		SetupOptions{AccessMode: "public", SmithVersion: "1.2.3"},
		io.Discard, io.Discard,
	); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	for _, flag := range []string{"--name", "--blueprint"} {
		if strings.Contains(conn.setupRunCmd, flag) {
			t.Errorf("setup command = %q, want no %s for a value the run does not have", conn.setupRunCmd, flag)
		}
	}
}

// ranAgainst reports whether a command running subcommand against scriptPath
// ran on the box.
func (f *fakeConn) ranAgainst(scriptPath, subcommand string) bool {
	for _, cmd := range f.runs {
		if strings.HasPrefix(cmd, fmt.Sprintf("bash %s %s", connection.ShellArg(scriptPath), subcommand)) {
			return true
		}
	}
	return false
}

// removed reports whether an rm of dir ran against the box.
func (f *fakeConn) removed(dir string) bool {
	for _, cmd := range f.runs {
		if cmd == "rm -rf -- "+connection.ShellArg(dir) {
			return true
		}
	}
	return false
}

func TestRunnerShipsOnceForPreflightAndSetup(t *testing.T) {
	conn := &fakeConn{preflightOut: preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04")}
	runner := NewRunner(conn)
	if _, err := runner.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if _, err := runner.Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if len(conn.copiedTo) != 1 {
		t.Fatalf("copied to %q, want bootstrap.sh shipped exactly once", conn.copiedTo)
	}
	shipped := conn.copiedTo[0]
	for _, sub := range []string{"preflight", "setup"} {
		if !conn.ranAgainst(shipped, sub) {
			t.Errorf("%s did not run against the shipped path %q; ran %q", sub, shipped, conn.runs)
		}
	}
	if got := runner.ScriptPath(); got != shipped {
		t.Errorf("ScriptPath() = %q, want the shipped path %q", got, shipped)
	}
	if path.Dir(shipped) == "/tmp" {
		t.Errorf("shipped to %q, a fixed name under /tmp, want a private directory", shipped)
	}
}

func TestRunnerCloseRemovesTheShippedScript(t *testing.T) {
	passing := preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04")
	tests := []struct {
		name string
		conn *fakeConn
		run  func(*Runner) error
	}{
		{
			name: "setup succeeds",
			conn: &fakeConn{preflightOut: passing},
			run:  preflightThenSetup,
		},
		{
			name: "a phase fails",
			conn: &fakeConn{preflightOut: passing, setupErr: fmt.Errorf("phase packages: %w", errRemote)},
			run:  preflightThenSetup,
		},
		{
			name: "preflight is refused",
			conn: &fakeConn{preflightOut: preflightOutput("none", "ubuntu", "24.04.1 LTS (Noble)", "24.04")},
			run: func(r *Runner) error {
				_, err := r.Preflight(context.Background())
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewRunner(tt.conn)
			if err := tt.run(runner); err != nil {
				t.Fatalf("run error = %v", err)
			}
			runner.Close(context.Background())
			if len(tt.conn.copiedTo) != 1 {
				t.Fatalf("copied to %q, want one shipped script", tt.conn.copiedTo)
			}
			if dir := path.Dir(tt.conn.copiedTo[0]); !tt.conn.removed(dir) {
				t.Errorf("shipped directory %q not removed; ran %q", dir, tt.conn.runs)
			}
		})
	}
}

func TestRunnerCloseBeforeShippingRemovesNothing(t *testing.T) {
	conn := &fakeConn{probeErr: fmt.Errorf("dial: %w", connection.ErrConnect)}
	runner := NewRunner(conn)
	if _, err := runner.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	runner.Close(context.Background())
	for _, cmd := range conn.runs {
		if strings.HasPrefix(cmd, "rm ") {
			t.Errorf("ran %q, want nothing removed when nothing was shipped", cmd)
		}
	}
}

func preflightThenSetup(r *Runner) error {
	if _, err := r.Preflight(context.Background()); err != nil {
		return err
	}
	_, err := r.Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	return err
}

// ownedBox fakes a box whose /tmp is sticky: it records which login owns each
// file and refuses a copy over a file another login owns, which is how a
// root-owned script left behind blocks the smith user.
type ownedBox struct {
	owner map[string]string
	dirs  int
}

// runnable reports whether cmd runs `bash <script> ...` against a script on the
// box, reading the script argument as a remote shell would, and returns what
// follows it.
func (b *ownedBox) runnable(cmd string) (string, bool) {
	for p := range b.owner {
		if rest, ok := strings.CutPrefix(cmd, "bash "+connection.ShellArg(p)+" "); ok {
			return rest, true
		}
	}
	return "", false
}

// login returns a connection to the box as user.
func (b *ownedBox) login(user string) *ownedConn { return &ownedConn{box: b, user: user} }

type ownedConn struct {
	box  *ownedBox
	user string
}

func (c *ownedConn) Copy(_ context.Context, _, remotePath string) error {
	if owner, ok := c.box.owner[remotePath]; ok && owner != c.user {
		return fmt.Errorf("scp %s: %s owned by %s: %w", c.user, remotePath, owner, errRemote)
	}
	if dir := path.Dir(remotePath); dir != "/tmp" && c.box.owner[dir] != c.user {
		return fmt.Errorf("scp %s: %s not writable: %w", c.user, dir, errRemote)
	}
	c.box.owner[remotePath] = c.user
	return nil
}

func (c *ownedConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	switch {
	case strings.HasPrefix(cmd, "mktemp"):
		c.box.dirs++
		dir := shippedDir(c.box.dirs)
		c.box.owner[dir] = c.user
		_, err := fmt.Fprintln(stdout, dir)
		return err
	case strings.HasPrefix(cmd, "bash "):
		subcommand, ok := c.box.runnable(cmd)
		if !ok {
			return fmt.Errorf("bash: %s: no such file: %w", cmd, errRemote)
		}
		if strings.HasPrefix(subcommand, "preflight") {
			_, err := io.WriteString(stdout, preflightOutput("root", "ubuntu", "24.04.1 LTS (Noble)", "24.04"))
			return err
		}
	}
	return nil
}

// TestSetupReRunAsSmithAfterARootSetup proves a re-run of setup as the smith
// user ships and runs after a root setup whose script was never cleaned up,
// rather than failing its copy on the file root left behind.
func TestSetupReRunAsSmithAfterARootSetup(t *testing.T) {
	box := &ownedBox{owner: map[string]string{}}
	if err := preflightThenSetup(NewRunner(box.login("root"))); err != nil {
		t.Fatalf("setup as root: %v", err)
	}

	runner := NewRunner(box.login("smith"))
	res, err := runner.Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight() as smith error = %v", err)
	}
	if res.Outcome != OutcomePassed {
		t.Fatalf("Preflight() as smith outcome = %v (%s), want Passed", res.Outcome, res.Reason)
	}
	setupRes, err := runner.Setup(context.Background(), SetupOptions{AccessMode: "public"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Setup() as smith error = %v", err)
	}
	if setupRes.Outcome != OutcomePassed {
		t.Errorf("Setup() as smith outcome = %v, want Passed", setupRes.Outcome)
	}
}
