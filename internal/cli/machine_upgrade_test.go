package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/provider"
	"github.com/byranZA/smith/internal/release"
)

// upgradeSSH stands in for the local ssh and scp an upgrade launches. It
// answers the box's probe with the machine hardware name and smith version the
// box reports, and records every remote command, so a test can assert on what
// reached the box.
type upgradeSSH struct {
	// machine is the machine hardware name `uname -m` prints on the box.
	machine string
	// installed is the smith version already on the box, empty when it has none.
	installed string
	// installErr is what the box answers the install command with, so a test can
	// drive a checksum mismatch or a failed download.
	installErr error

	commands []string
	targets  []string
}

func (s *upgradeSSH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name != "ssh" || len(args) < 2 {
		return nil
	}
	target, remoteCmd := args[len(args)-2], args[len(args)-1]
	s.targets = append(s.targets, target)
	s.commands = append(s.commands, remoteCmd)
	if answered, err := answerVersionCheck(remoteCmd, stdout); answered {
		return err
	}
	if strings.Contains(remoteCmd, " probe") {
		out := "arch=" + s.machine + "\n"
		if s.installed != "" {
			out += "smith-version=" + s.installed + "\n"
		}
		_, err := io.WriteString(stdout, out)
		return err
	}
	if strings.Contains(remoteCmd, " install ") {
		return s.installErr
	}
	return nil
}

// installCommand returns the install command the box was asked to run, empty
// when nothing was downloaded.
func (s *upgradeSSH) installCommand() string {
	for _, cmd := range s.commands {
		if strings.Contains(cmd, " install ") {
			return cmd
		}
	}
	return ""
}

// runUpgrade runs `machine upgrade` against a config home rooted at dir, with
// every local binary answered by the given fake.
func runUpgrade(t *testing.T, dir string, ssh *upgradeSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"upgrade"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// asReleaseBuild makes this smith the given release build for one test, the way
// a pipeline build's ldflags would, and restores the dev fallback afterwards.
func asReleaseBuild(t *testing.T, version string) {
	t.Helper()
	previous := buildVersion
	buildVersion = version
	t.Cleanup(func() { buildVersion = previous })
}

// registeredBox is a config home holding one box registered as "dev".
func registeredBox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	return dir
}

func TestUpgradeConvergesTheBoxToLocalSmithsVersion(t *testing.T) {
	asReleaseBuild(t, "0.2.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.1.0"}

	stdout, stderr, code := runUpgrade(t, dir, ssh, "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if cmd := ssh.installCommand(); !strings.Contains(cmd, release.For("0.2.0", "amd64").URL) {
		t.Errorf("install command %q does not fetch the 0.2.0 amd64 asset", cmd)
	}
	for _, want := range []string{"dev", "0.1.0", "0.2.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
	if len(ssh.targets) == 0 || ssh.targets[0] != "smith@100.92.14.7" {
		t.Errorf("upgrade reached %v, want the registered target", ssh.targets)
	}
}

func TestUpgradeMovesABoxBackwardsAndSaysItMatchesLocalSmith(t *testing.T) {
	asReleaseBuild(t, "0.1.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.3.0"}

	stdout, stderr, code := runUpgrade(t, dir, ssh, "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"0.3.0", "0.1.0", "matching local smith"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
}

func TestUpgradeLeavesABoxAlreadyMatchingAlone(t *testing.T) {
	asReleaseBuild(t, "0.2.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.2.0"}

	stdout, stderr, code := runUpgrade(t, dir, ssh, "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if cmd := ssh.installCommand(); cmd != "" {
		t.Errorf("the box downloaded %q, want nothing downloaded", cmd)
	}
	if !strings.Contains(stdout, "already") {
		t.Errorf("stdout = %q, does not report the box as already matching", stdout)
	}
}

func TestUpgradeRefusesADevBuildNamingTheFlagAndBothSides(t *testing.T) {
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64"}

	_, stderr, code := runUpgrade(t, dir, ssh, "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want a dev build refused")
	}
	for _, want := range []string{"dev build", "--smith-version", "relay", "released"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("refusal = %q, does not mention %q", stderr, want)
		}
	}
	if len(ssh.commands) != 0 {
		t.Errorf("the box was reached with %v, want it untouched", ssh.commands)
	}
}

func TestUpgradeVersionFlagOverridesTheBuildItRunsFrom(t *testing.T) {
	tests := []struct {
		name         string
		buildVersion string
	}{
		{"a dev build", "dev"},
		{"a release build", "0.2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asReleaseBuild(t, tt.buildVersion)
			dir := registeredBox(t)
			ssh := &upgradeSSH{machine: "x86_64"}

			_, stderr, code := runUpgrade(t, dir, ssh, "dev", "--smith-version", "0.1.0")

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			if cmd := ssh.installCommand(); !strings.Contains(cmd, release.For("0.1.0", "amd64").URL) {
				t.Errorf("install command %q does not fetch the 0.1.0 asset", cmd)
			}
		})
	}
}

func TestUpgradeRefusesAnUnsupportedArchitectureByName(t *testing.T) {
	asReleaseBuild(t, "0.2.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "riscv64"}

	_, stderr, code := runUpgrade(t, dir, ssh, "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want an unsupported architecture refused")
	}
	if !strings.Contains(stderr, "riscv64") {
		t.Errorf("refusal = %q, does not name the machine hardware name", stderr)
	}
	if cmd := ssh.installCommand(); cmd != "" {
		t.Errorf("the box was asked to install %q, want nothing installed", cmd)
	}
}

func TestUpgradeFailsWhenTheBoxRefusesTheDownload(t *testing.T) {
	asReleaseBuild(t, "0.2.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.1.0", installErr: errors.New("checksum mismatch")}

	_, _, code := runUpgrade(t, dir, ssh, "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want a failed install reported")
	}
}

func TestUpgradeConvergesNothingButTheBinary(t *testing.T) {
	asReleaseBuild(t, "0.2.0")
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.1.0"}

	if _, stderr, code := runUpgrade(t, dir, ssh, "dev"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	for _, cmd := range ssh.commands {
		for _, forbidden := range []string{"bootstrap.sh", "/etc/smith"} {
			if strings.Contains(cmd, forbidden) {
				t.Errorf("upgrade ran %q, which reaches %q: it converges the binary and nothing else", cmd, forbidden)
			}
		}
		// The one verb an upgrade relays is the install's own confirmation:
		// the binary it just wrote, asked what version it is.
		if strings.Contains(cmd, "--relayed-from") && !strings.HasSuffix(cmd, "'version'") {
			t.Errorf("upgrade relayed %q, want nothing relayed but the version check that confirms the install", cmd)
		}
	}
}
