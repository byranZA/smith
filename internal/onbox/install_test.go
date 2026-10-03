package onbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/release"
	"github.com/byranZA/smith/internal/shipped"
)

// relayConn stands in for the connection the installer confirms over: it
// answers a relayed version check and records every command relayed to it.
type relayConn struct {
	// reports is the version the box's binary answers the relayed version
	// check with. Empty is the box that agrees with the smith that installed
	// it, which is what a successful install leaves behind.
	reports  string
	commands []string
}

func (c *relayConn) Run(_ context.Context, remoteCmd string, stdout, _ io.Writer) error {
	c.commands = append(c.commands, remoteCmd)
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
	_, rest, ok := strings.Cut(remoteCmd, "--relayed-from '")
	if !ok {
		return ""
	}
	version, _, _ := strings.Cut(rest, "'")
	return version
}

// installScript returns a recording install.sh whose probe reports machine and,
// when installed is not empty, the smith version already on the box.
func installScript(machine, installed string) *shipped.Fake {
	out := "arch=" + machine + "\n"
	if installed != "" {
		out += "smith-version=" + installed + "\n"
	}
	return &shipped.Fake{Replies: map[string]shipped.Reply{"probe": {Stdout: out}}}
}

// installCall returns the install subcommand the script was asked to run, and
// whether it was asked at all.
func installCall(script *shipped.Fake) (shipped.Call, bool) {
	i := slices.IndexFunc(script.Calls, func(c shipped.Call) bool { return c.Sub == "install" })
	if i < 0 {
		return shipped.Call{}, false
	}
	return script.Calls[i], true
}

// converge runs the install stage for box "dev" through script, confirming over
// conn.
func converge(conn *relayConn, script *shipped.Fake, version string) (Result, error) {
	return NewInstaller(conn, script, "dev").Converge(context.Background(), version)
}

func TestConvergeInstallsTheAssetForTheBoxsArchitecture(t *testing.T) {
	script := installScript("x86_64", "")

	got, err := converge(&relayConn{}, script, "0.2.0")
	if err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	call, _ := installCall(script)
	want := []string{
		"--url", "https://github.com/byranZA/smith/releases/download/v0.2.0/smith_0.2.0_linux_amd64.tar.gz",
		"--checksums-url", "https://github.com/byranZA/smith/releases/download/v0.2.0/checksums.txt",
	}
	if !slices.Equal(call.Args, want) {
		t.Errorf("install args = %q, want %q", call.Args, want)
	}
	if !got.Changed || got.Version != "0.2.0" || got.Previous != "" {
		t.Errorf("Converge() = %+v, want the box installed at 0.2.0 from nothing", got)
	}
}

func TestConvergeReadsTheArchitectureFromTheBox(t *testing.T) {
	script := installScript("aarch64", "")

	if _, err := converge(&relayConn{}, script, "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	call, _ := installCall(script)
	want := "https://github.com/byranZA/smith/releases/download/v0.2.0/smith_0.2.0_linux_arm64.tar.gz"
	if !slices.Contains(call.Args, want) {
		t.Errorf("install args %q do not fetch the arm64 asset %q", call.Args, want)
	}
}

func TestConvergeHandsTheScriptAURLWithShellMetacharactersVerbatim(t *testing.T) {
	const version = "0.2.0 it's; $(not run) `not run`"
	script := installScript("x86_64", "")

	if _, err := converge(&relayConn{reports: version}, script, version); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	call, _ := installCall(script)
	want := "https://github.com/byranZA/smith/releases/download/v0.2.0 it's; $(not run) `not run`/" +
		"smith_0.2.0 it's; $(not run) `not run`_linux_amd64.tar.gz"
	if len(call.Args) < 2 || call.Args[1] != want {
		t.Errorf("install args = %q, want the URL %q as one argument", call.Args, want)
	}
}

func TestConvergeRefusesAnUnsupportedArchitectureByName(t *testing.T) {
	script := installScript("riscv64", "")

	_, err := converge(&relayConn{}, script, "0.2.0")

	var unsupported *release.UnsupportedArchError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Converge() error = %v, want an *UnsupportedArchError", err)
	}
	if !strings.Contains(err.Error(), "riscv64") {
		t.Errorf("error %q does not name the machine hardware name", err)
	}
	if call, ok := installCall(script); ok {
		t.Errorf("the box was asked to install %q, want nothing installed", call.Args)
	}
}

func TestConvergeLeavesABoxAlreadyAtTheVersionAlone(t *testing.T) {
	script := installScript("x86_64", "0.2.0")

	got, err := converge(&relayConn{}, script, "0.2.0")
	if err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if call, ok := installCall(script); ok {
		t.Errorf("the box downloaded %q, want nothing downloaded", call.Args)
	}
	if got.Changed {
		t.Errorf("Converge() = %+v, want the box reported as already matching", got)
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
			script := installScript("x86_64", tt.installed)

			got, err := converge(&relayConn{}, script, "0.2.0")
			if err != nil {
				t.Fatalf("Converge() errored: %v", err)
			}

			if _, ok := installCall(script); !ok {
				t.Errorf("subcommands = %+v, want the 0.2.0 asset installed", script.Calls)
			}
			if !got.Changed || got.Previous != tt.installed || got.Version != "0.2.0" {
				t.Errorf("Converge() = %+v, want %s converged to 0.2.0", got, tt.installed)
			}
		})
	}
}

func TestConvergeClosesTheShippedScript(t *testing.T) {
	tests := []struct {
		name   string
		script *shipped.Fake
	}{
		{"success", installScript("x86_64", "")},
		{"a failed install", &shipped.Fake{Replies: map[string]shipped.Reply{
			"probe":   {Stdout: "arch=x86_64\n"},
			"install": {Err: errors.New("exit status 1")},
		}}},
		{"a box already at the version", installScript("x86_64", "0.2.0")},
		{"a refused architecture", installScript("riscv64", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converge(&relayConn{}, tt.script, "0.2.0")

			if !tt.script.Closed {
				t.Errorf("Converge() = %+v, %v left the shipped script open, want it closed", got, err)
			}
		})
	}
}

func TestConvergeKeepsWhyTheScriptWasNotShipped(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"a box that refuses the connection", connection.ErrConnect},
		{"a box that refuses the ship", shipped.ErrNotShipped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := &shipped.Fake{Replies: map[string]shipped.Reply{"probe": {Err: tt.err}}}

			_, err := converge(&relayConn{}, script, "0.2.0")

			if !errors.Is(err, tt.err) {
				t.Errorf("Converge() error = %v, want it to wrap %v", err, tt.err)
			}
		})
	}
}

// TestConvergeReportsAFailedInstall checks that the operator is left with what
// the box said, not just how it exited, and that an install which ran and
// failed is not mistaken for one that never shipped.
func TestConvergeReportsAFailedInstall(t *testing.T) {
	script := &shipped.Fake{Replies: map[string]shipped.Reply{
		"probe":   {Stdout: "arch=x86_64\nsmith-version=0.1.0\n"},
		"install": {Stderr: "checksum mismatch: nothing was installed\n", Err: errors.New("exit status 1")},
	}}

	_, err := converge(&relayConn{}, script, "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the failure reported")
	}
	if !strings.Contains(err.Error(), "checksum mismatch: nothing was installed") {
		t.Errorf("error %q does not carry what the box reported", err)
	}
	if errors.Is(err, shipped.ErrNotShipped) {
		t.Errorf("error %q reports an install that ran as never shipped", err)
	}
}

// TestConvergeConfirmsTheInstallByRelayingAVersionCheck checks the install
// stage proving what it just did rather than assuming it: the binary it
// installed is asked its version through the relay, declaring the version that
// installed it, so the two sides are shown to agree end to end.
func TestConvergeConfirmsTheInstallByRelayingAVersionCheck(t *testing.T) {
	conn := &relayConn{}

	if _, err := converge(conn, installScript("x86_64", ""), "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if len(conn.commands) != 1 {
		t.Fatalf("relayed %q, want the install confirmed by one relayed version check", conn.commands)
	}
	for _, want := range []string{InstallPath, "--relayed-from '0.2.0'", "'version'"} {
		if !strings.Contains(conn.commands[0], want) {
			t.Errorf("confirmation %q does not contain %q", conn.commands[0], want)
		}
	}
}

// TestProbeDeclaresNoRelayedVersion guards the asymmetry that keeps the stage
// able to converge anything: a skewed box would refuse the very probe that
// detects the skew.
func TestProbeDeclaresNoRelayedVersion(t *testing.T) {
	script := installScript("x86_64", "0.1.0")

	if _, err := converge(&relayConn{}, script, "0.2.0"); err != nil {
		t.Fatalf("Converge() errored: %v", err)
	}

	if probe := script.Calls[0]; probe.Sub != "probe" || len(probe.Args) != 0 {
		t.Errorf("first subcommand = %+v, want a bare probe", probe)
	}
}

// TestConvergeFailsWhenTheConfirmationDisagrees checks that a binary reporting
// a version other than the one installed fails the stage rather than being
// reported as installed.
func TestConvergeFailsWhenTheConfirmationDisagrees(t *testing.T) {
	_, err := converge(&relayConn{reports: "0.1.0"}, installScript("x86_64", ""), "0.2.0")

	if err == nil {
		t.Fatal("Converge() succeeded, want the disagreeing binary to fail the stage")
	}
	for _, want := range []string{"0.1.0", "0.2.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestResultReportSaysWhatHappenedToTheBinary(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   string
	}{
		{"already matching", Result{Version: "0.2.0", Previous: "0.2.0"}, "smith 0.2.0 already matches local smith\n"},
		{"installed from nothing", Result{Version: "0.2.0", Changed: true}, "installed smith 0.2.0\n"},
		{"moved backwards", Result{Version: "0.2.0", Previous: "0.3.0", Changed: true}, "smith 0.3.0 → 0.2.0 (matching local smith)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Report(); got != tt.want {
				t.Errorf("Report() = %q, want %q", got, tt.want)
			}
		})
	}
}
