package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
)

// releaseRun is the flag pair a run of a build that has a release is driven
// with: the tests here drive a dev build (a test binary reports no released
// tag), so the version the install stage fetches is named explicitly.
func releaseRun(version string) []string { return []string{"--smith-version", version} }

// commandIndex returns the position of the first remote command containing
// want, or -1 when the box was never asked to run one.
func commandIndex(commands []string, want string) int {
	return slices.IndexFunc(commands, func(cmd string) bool { return strings.Contains(cmd, want) })
}

func TestSetupTakesTheSmithVersionToInstall(t *testing.T) {
	cmd := newSetupCmd(func() (config.Home, error) { return config.NewHome(t.TempDir()), nil }, connection.System())
	if cmd.Flags().Lookup("smith-version") == nil {
		t.Error("machine setup has no --smith-version flag, so a build with no release can never set a box up")
	}
}

func TestSetupInstallsTheBinaryOnlyAfterEveryPhaseCompleted(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64"}

	_, stderr, code := runSetup(t, t.TempDir(), ssh, append(releaseRun("0.2.0"), "root@203.0.113.10")...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	phases, install := commandIndex(ssh.commands, "bootstrap.sh setup"), commandIndex(ssh.commands, "smith-install.sh install ")
	if phases < 0 || install < 0 {
		t.Fatalf("commands = %v, want the phases and then the install stage", ssh.commands)
	}
	if phases > install {
		t.Errorf("commands = %v, want every phase to complete before the install stage began", ssh.commands)
	}
}

func TestSetupInstallsTheVersionTheOperatorNamed(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64"}

	_, stderr, code := runSetup(t, t.TempDir(), ssh, append(releaseRun("0.2.0"), "root@203.0.113.10")...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if i := commandIndex(ssh.commands, "smith_0.2.0_linux_amd64.tar.gz"); i < 0 {
		t.Errorf("commands = %v, want the release asset for the version the operator named", ssh.commands)
	}
}

func TestSetupDownloadsNothingWhenAPhaseFailed(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64", phaseErr: refusedExit{}}

	_, _, code := runSetup(t, t.TempDir(), ssh, append(releaseRun("0.2.0"), "root@203.0.113.10")...)

	if code == 0 {
		t.Fatal("exit code = 0, want a failed phase to fail the run")
	}
	if i := commandIndex(ssh.commands, "smith-install.sh"); i >= 0 {
		t.Errorf("commands = %v, want a failed bootstrap to never reach the install stage", ssh.commands)
	}
}

func TestSetupUnderADevBuildBootstrapsTheBoxAndFailsOnlyAtTheInstallStage(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64"}

	_, stderr, code := runSetupAsBuilt(t, t.TempDir(), ssh, "root@203.0.113.10")

	if code == 0 {
		t.Fatal("exit code = 0, want a dev build refused at the install stage")
	}
	if i := commandIndex(ssh.commands, "bootstrap.sh setup"); i < 0 {
		t.Errorf("commands = %v, want a dev build to still bootstrap the box", ssh.commands)
	}
	if i := commandIndex(ssh.commands, "smith-install.sh install "); i >= 0 {
		t.Errorf("commands = %v, want nothing installed from a build with no release", ssh.commands)
	}
	for _, want := range []string{"install", "stage", "dev build", "--smith-version", "phase"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to name %q", stderr, want)
		}
	}
}

func TestSetupRunsTheAccessStageBeforeTheInstallStage(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64", tailnetIP: "100.92.14.7"}

	args := append(tailscaleRun(t), append(releaseRun("0.2.0"), "root@203.0.113.10")...)
	_, stderr, code := runSetup(t, t.TempDir(), ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	access, install := commandIndex(ssh.commands, "tailscale-status"), commandIndex(ssh.commands, "smith-install.sh install ")
	if access < 0 || install < 0 {
		t.Fatalf("commands = %v, want the access stage and then the install stage", ssh.commands)
	}
	if access > install {
		t.Errorf("commands = %v, want the access stage to run before the install stage", ssh.commands)
	}
}

// TestSetupRecordsNothingAboutTheInstalledBinaryOnTheMarker pins the marker's
// meaning: smith_version is the smith that provisioned the box, not the one the
// install stage put on it, so naming a version to install never changes it.
func TestSetupRecordsNothingAboutTheInstalledBinaryOnTheMarker(t *testing.T) {
	ssh := &setupSSH{machine: "x86_64"}

	_, stderr, code := runSetup(t, t.TempDir(), ssh, append(releaseRun("0.2.0"), "root@203.0.113.10")...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	i := commandIndex(ssh.commands, "bootstrap.sh setup")
	if i < 0 {
		t.Fatalf("commands = %v, want the phases to have run", ssh.commands)
	}
	if strings.Contains(ssh.commands[i], "0.2.0") {
		t.Errorf("setup command = %q, want the marker to record the smith that provisioned the box", ssh.commands[i])
	}
}
