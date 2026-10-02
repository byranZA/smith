package onbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/release"
)

// boxConn stands in for the connection to a box: it answers the installer's
// probe with what the box reports and records every remote command it was
// asked to run, so a test can assert on what reached the box rather than on
// how the installer got there.
type boxConn struct {
	// machine is the machine hardware name `uname -m` prints on the box.
	machine string
	// installed is the smith version already on the box, empty when it has none.
	installed string
	// installErr is what the install step answers with, so a test can drive a
	// download or verification failure.
	installErr error
	// installDiagnostic is what the box prints to stderr while the install
	// fails, the way the install script explains an abort.
	installDiagnostic string
	// reports is the version the box's binary answers the relayed version
	// check with. Empty is the box that agrees with the smith that installed
	// it, which is what a successful install leaves behind.
	reports string

	// copyErr is what the box answers a copy with, so a test can drive a
	// refused ship.
	copyErr error
	// user is the login the connection reaches the box as; empty is "smith".
	user string
	// owner records which login owns each file and directory on the box. As a
	// sticky /tmp would, a copy over a file another login owns, or into a
	// directory this login does not own, is refused.
	owner map[string]string
	// dirs counts the private directories mktemp has handed out.
	dirs int

	commands []string
	shipped  []string
	removed  []string
}

func (c *boxConn) login() string {
	if c.user == "" {
		return "smith"
	}
	return c.user
}

func (c *boxConn) Copy(_ context.Context, _, remotePath string) error {
	if c.copyErr != nil {
		return c.copyErr
	}
	if c.owner == nil {
		c.owner = map[string]string{}
	}
	if owner, ok := c.owner[remotePath]; ok && owner != c.login() {
		return fmt.Errorf("scp %s: %s owned by %s: exit status 1", c.login(), remotePath, owner)
	}
	if dir := path.Dir(remotePath); dir != "/tmp" && c.owner[dir] != c.login() {
		return fmt.Errorf("scp %s: %s not writable: exit status 1", c.login(), dir)
	}
	c.owner[remotePath] = c.login()
	c.shipped = append(c.shipped, remotePath)
	return nil
}

// leftovers lists the paths still on the box under dir.
func (c *boxConn) leftovers(dir string) []string {
	var paths []string
	for p := range c.owner {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			paths = append(paths, p)
		}
	}
	return paths
}

func (c *boxConn) Run(_ context.Context, remoteCmd string, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, remoteCmd)
	if c.owner == nil {
		c.owner = map[string]string{}
	}
	switch {
	case strings.HasPrefix(remoteCmd, "mktemp -d"):
		c.dirs++
		dir := fmt.Sprintf("/tmp/smith.%08d", c.dirs)
		c.owner[dir] = c.login()
		_, err := fmt.Fprintln(stdout, dir)
		return err
	case strings.HasPrefix(remoteCmd, "rm -rf -- "):
		dir := strings.Trim(strings.TrimPrefix(remoteCmd, "rm -rf -- "), "'")
		for _, p := range c.leftovers(dir) {
			delete(c.owner, p)
		}
		c.removed = append(c.removed, dir)
		return nil
	case strings.HasPrefix(remoteCmd, "bash "):
		script, _, _ := strings.Cut(strings.TrimPrefix(remoteCmd, "bash "), " ")
		if _, ok := c.owner[script]; !ok {
			return fmt.Errorf("bash: %s: no such file", script)
		}
	}
	if strings.Contains(remoteCmd, " probe") {
		out := "arch=" + c.machine + "\n"
		if c.installed != "" {
			out += "smith-version=" + c.installed + "\n"
		}
		_, err := io.WriteString(stdout, out)
		return err
	}
	if strings.Contains(remoteCmd, relayedFromFlag) {
		return c.confirm(remoteCmd, stdout)
	}
	if c.installDiagnostic != "" {
		if _, err := io.WriteString(stderr, c.installDiagnostic); err != nil {
			return fmt.Errorf("write the box's diagnostic: %w", err)
		}
	}
	return c.installErr
}

// confirm answers a relayed version check the way a box whose binary agrees
// with the smith that installed it does, unless a test named a version for it
// to disagree with.
func (c *boxConn) confirm(remoteCmd string, stdout io.Writer) error {
	version := c.reports
	if version == "" {
		version = declaredVersion(remoteCmd)
	}
	if _, err := fmt.Fprintf(stdout, "smith %s\n", version); err != nil {
		return fmt.Errorf("write the box's version: %w", err)
	}
	return nil
}

// declaredVersion reads the version a relayed command declared, as the box's
// own smith would.
func declaredVersion(remoteCmd string) string {
	_, rest, ok := strings.Cut(remoteCmd, relayedFromFlag+" '")
	if !ok {
		return ""
	}
	version, _, _ := strings.Cut(rest, "'")
	return version
}

// relayedFromFlag is the flag a relayed command carries, spelled here as the
// box's shell sees it.
const relayedFromFlag = "--relayed-from"

// relayedCommand returns the relayed command the box was asked to run, empty
// when nothing was relayed to it.
func (c *boxConn) relayedCommand() string {
	for _, cmd := range c.commands {
		if strings.Contains(cmd, relayedFromFlag) {
			return cmd
		}
	}
	return ""
}

// probeCommand returns the command that read the box's state, empty when it was
// never probed.
func (c *boxConn) probeCommand() string {
	for _, cmd := range c.commands {
		if strings.Contains(cmd, " probe") {
			return cmd
		}
	}
	return ""
}

// installCommand returns the install command the box was asked to run, empty
// when the installer never asked it to install anything.
func (c *boxConn) installCommand() string {
	for _, cmd := range c.commands {
		if strings.Contains(cmd, " install ") {
			return cmd
		}
	}
	return ""
}

func TestConvergeInstallsTheAssetForTheBoxsArchitecture(t *testing.T) {
	conn := &boxConn{machine: "x86_64"}

	got, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")
	if err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	want := release.For("0.2.0", "amd64")
	cmd := conn.installCommand()
	if !strings.Contains(cmd, want.URL) {
		t.Errorf("install command %q does not fetch %q", cmd, want.URL)
	}
	if !strings.Contains(cmd, want.ChecksumsURL) {
		t.Errorf("install command %q does not fetch the checksums at %q", cmd, want.ChecksumsURL)
	}
	if !got.Changed || got.Version != "0.2.0" || got.Previous != "" {
		t.Errorf("Converge() = %+v, want the box installed at 0.2.0 from nothing", got)
	}
	if report := got.Report(); !strings.Contains(report, "0.2.0") {
		t.Errorf("Report() = %q, does not name the installed version", report)
	}
}

func TestConvergeReadsTheArchitectureFromTheBox(t *testing.T) {
	conn := &boxConn{machine: "aarch64"}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	want := release.For("0.2.0", "arm64")
	if cmd := conn.installCommand(); !strings.Contains(cmd, want.URL) {
		t.Errorf("install command %q does not fetch the arm64 asset %q", cmd, want.URL)
	}
}

func TestConvergeRefusesAnUnsupportedArchitectureByName(t *testing.T) {
	conn := &boxConn{machine: "riscv64"}

	_, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")

	var unsupported *release.UnsupportedArchError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Converge() error = %v, want an *UnsupportedArchError", err)
	}
	if !strings.Contains(err.Error(), "riscv64") {
		t.Errorf("error %q does not name the machine hardware name", err)
	}
	if cmd := conn.installCommand(); cmd != "" {
		t.Errorf("the box was asked to install %q, want nothing installed", cmd)
	}
}

func TestConvergeLeavesABoxAlreadyAtTheVersionAlone(t *testing.T) {
	conn := &boxConn{machine: "x86_64", installed: "0.2.0"}

	got, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")
	if err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if cmd := conn.installCommand(); cmd != "" {
		t.Errorf("the box downloaded %q, want nothing downloaded", cmd)
	}
	if got.Changed {
		t.Errorf("Converge() = %+v, want the box reported as already matching", got)
	}
	if report := got.Report(); !strings.Contains(report, "already") {
		t.Errorf("Report() = %q, does not report the box as already matching", report)
	}
}

func TestConvergeMovesABoxToLocalsVersionInEitherDirection(t *testing.T) {
	tests := []struct {
		name      string
		installed string
	}{
		{"a box behind local smith", "0.1.0"},
		{"a box ahead of local smith", "0.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &boxConn{machine: "x86_64", installed: tt.installed}

			got, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")
			if err != nil {
				t.Fatalf("Converge() errored: %v", err)
			}

			if cmd := conn.installCommand(); !strings.Contains(cmd, release.For("0.2.0", "amd64").URL) {
				t.Errorf("install command %q does not fetch the 0.2.0 asset", cmd)
			}
			if !got.Changed || got.Previous != tt.installed || got.Version != "0.2.0" {
				t.Errorf("Converge() = %+v, want %s converged to 0.2.0", got, tt.installed)
			}
			report := got.Report()
			for _, want := range []string{tt.installed, "0.2.0", "matching local smith"} {
				if !strings.Contains(report, want) {
					t.Errorf("Report() = %q, does not contain %q", report, want)
				}
			}
		})
	}
}

// scriptRuns returns the script paths every `bash <script> <subcommand>` the
// box was asked to run went against.
func (c *boxConn) scriptRuns() []string {
	var scripts []string
	for _, cmd := range c.commands {
		if rest, ok := strings.CutPrefix(cmd, "bash "); ok {
			script, _, _ := strings.Cut(rest, " ")
			scripts = append(scripts, script)
		}
	}
	return scripts
}

func TestConvergeShipsTheInstallScriptOnceForProbeAndInstall(t *testing.T) {
	conn := &boxConn{machine: "x86_64"}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if len(conn.shipped) != 1 {
		t.Fatalf("shipped %v, want the install script shipped once", conn.shipped)
	}
	shipped := conn.shipped[0]
	if path.Dir(shipped) == "/tmp" {
		t.Errorf("shipped to %q, a fixed name under /tmp, want a private directory", shipped)
	}
	runs := conn.scriptRuns()
	if len(runs) != 2 {
		t.Fatalf("ran %v, want probe and install", conn.commands)
	}
	for _, script := range runs {
		if script != shipped {
			t.Errorf("ran a subcommand against %q, want the shipped %q", script, shipped)
		}
	}
}

func TestConvergeRemovesTheShippedScript(t *testing.T) {
	tests := []struct {
		name string
		conn *boxConn
	}{
		{"success", &boxConn{machine: "x86_64"}},
		{"a checksum mismatch", &boxConn{
			machine:           "x86_64",
			installErr:        errors.New("exit status 1"),
			installDiagnostic: "checksum mismatch: nothing was installed\n",
		}},
		{"a box already at the version", &boxConn{machine: "x86_64", installed: "0.2.0"}},
		{"a refused architecture", &boxConn{machine: "riscv64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The outcome varies by case; only what is left on the box matters here.
			got, err := NewInstaller(tt.conn, "dev").Converge(context.Background(), "0.2.0")
			if len(tt.conn.shipped) != 1 {
				t.Fatalf("shipped %v (Converge = %+v, %v), want one ship", tt.conn.shipped, got, err)
			}
			if left := tt.conn.leftovers(path.Dir(tt.conn.shipped[0])); len(left) != 0 {
				t.Errorf("left %q on the box, want the shipped directory removed", left)
			}
		})
	}
}

// TestConvergeWorksAsSmithAfterARootSetup is the ownership regression: a root
// setup on v0.2.0-rc.1 left install.sh root-owned at a fixed /tmp path, which
// the smith login can neither replace nor delete under a sticky /tmp.
func TestConvergeWorksAsSmithAfterARootSetup(t *testing.T) {
	const stale = "/tmp/smith-install.sh"
	conn := &boxConn{machine: "x86_64", installed: "0.1.0", user: "smith", owner: map[string]string{stale: "root"}}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() as smith errored: %v", err)
	}
	if owner := conn.owner[stale]; owner != "root" {
		t.Errorf("stale %s owned by %q, want it left untouched as root's", stale, owner)
	}
}

func TestConvergeFailsWhenTheShipIsRefused(t *testing.T) {
	conn := &boxConn{machine: "x86_64", copyErr: errors.New("scp: exit status 1")}

	_, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the refused copy reported")
	}
	if runs := conn.scriptRuns(); len(runs) != 0 {
		t.Errorf("ran %v, want nothing run without a shipped script", runs)
	}
}

// TestConvergeReportsAFailedInstall checks that the operator is left with what
// the box said, not just how it exited: the connection's error carries the exit
// status alone, and the explanation arrives on the box's stderr.
func TestConvergeReportsAFailedInstall(t *testing.T) {
	conn := &boxConn{
		machine:           "x86_64",
		installed:         "0.1.0",
		installErr:        errors.New("exit status 1"),
		installDiagnostic: "checksum mismatch: nothing was installed\n",
	}

	_, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the failure reported")
	}
	if !strings.Contains(err.Error(), "checksum mismatch: nothing was installed") {
		t.Errorf("error %q does not carry what the box reported", err)
	}
}

// TestConvergeConfirmsTheInstallByRelayingAVersionCheck checks the install
// stage proving what it just did rather than assuming it: the binary it
// installed is asked its version through the relay, declaring the version that
// installed it, so the two sides are shown to agree end to end.
func TestConvergeConfirmsTheInstallByRelayingAVersionCheck(t *testing.T) {
	conn := &boxConn{machine: "x86_64"}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	cmd := conn.relayedCommand()
	if cmd == "" {
		t.Fatalf("commands = %v, want the install confirmed by a relayed version check", conn.commands)
	}
	for _, want := range []string{InstallPath, "--relayed-from '0.2.0'", "'version'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("confirmation %q does not contain %q", cmd, want)
		}
	}
}

// TestProbeDeclaresNoRelayedVersion guards the asymmetry that keeps the stage
// able to converge anything: a skewed box would refuse the very probe that
// detects the skew.
func TestProbeDeclaresNoRelayedVersion(t *testing.T) {
	conn := &boxConn{machine: "x86_64", installed: "0.1.0"}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if cmd := conn.probeCommand(); strings.Contains(cmd, relayedFromFlag) {
		t.Errorf("probe %q declares a relayed version, want it undeclared", cmd)
	}
}

// TestConvergeFailsWhenTheConfirmationDisagrees checks that a binary reporting
// a version other than the one installed fails the stage rather than being
// reported as installed.
func TestConvergeFailsWhenTheConfirmationDisagrees(t *testing.T) {
	conn := &boxConn{machine: "x86_64", reports: "0.1.0"}

	_, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the disagreeing binary to fail the stage")
	}
	for _, want := range []string{"0.1.0", "0.2.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestConvergeInstallsOntoABoxThatNeverHadSmith checks the state every box
// provisioned before this stage is in: nothing installed, and an upgrade that
// installs rather than refusing.
func TestConvergeInstallsOntoABoxThatNeverHadSmith(t *testing.T) {
	conn := &boxConn{machine: "x86_64"}

	got, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")
	if err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if !got.Changed || got.Version != "0.2.0" || got.Previous != "" {
		t.Errorf("Converge() = %+v, want smith 0.2.0 installed onto a box that had none", got)
	}
}
