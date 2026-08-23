package onbox

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// reports is the version the box's binary answers the relayed version
	// check with. Empty is the box that agrees with the smith that installed
	// it, which is what a successful install leaves behind.
	reports string

	commands []string
	shipped  []string
}

func (c *boxConn) Copy(_ context.Context, _, remotePath string) error {
	c.shipped = append(c.shipped, remotePath)
	return nil
}

func (c *boxConn) Run(_ context.Context, remoteCmd string, stdout, _ io.Writer) error {
	c.commands = append(c.commands, remoteCmd)
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

func TestConvergeShipsItsOwnScriptRatherThanTheBootstrapScript(t *testing.T) {
	conn := &boxConn{machine: "x86_64"}

	if _, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if len(conn.shipped) != 1 || conn.shipped[0] != RemoteScriptPath {
		t.Errorf("shipped %v, want the install script at %q alone", conn.shipped, RemoteScriptPath)
	}
}

func TestConvergeReportsAFailedInstall(t *testing.T) {
	conn := &boxConn{machine: "x86_64", installed: "0.1.0", installErr: errors.New("checksum mismatch")}

	_, err := NewInstaller(conn, "dev").Converge(context.Background(), "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the failure reported")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
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
