package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScriptProbeReportsLiveFacts runs the embedded bootstrap.sh probe subcommand
// against fake privileged binaries and a provisioned box's marker, and checks it
// emits the marker block and each live fact read-only. It is the box-side half of
// the status drift report: the Go prober parses exactly these lines.
func TestScriptProbeReportsLiveFacts(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "bootstrap.sh")
	if err := os.WriteFile(scriptPath, []byte(Script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeProbeFakeBins(t, binDir)

	// A provisioned box: a marker and an auto-upgrades config the probe reads.
	markerPath := filepath.Join(dir, "bootstrap.json")
	if err := os.WriteFile(markerPath, []byte(`{"schema_version":1,"access_mode":"public","completed_phases":["packages","smith-user"]}`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	autoUpgradesConf := filepath.Join(dir, "20auto-upgrades")
	if err := os.WriteFile(autoUpgradesConf, []byte(`APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
`), 0o644); err != nil {
		t.Fatalf("write auto-upgrades conf: %v", err)
	}

	env := append(os.Environ()[:0:0],
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SMITH_MARKER="+markerPath,
		"SMITH_AUTO_UPGRADES_CONF="+autoUpgradesConf,
	)
	cmd := exec.Command(bash, scriptPath, "probe")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe run failed: %v\n%s", err, out)
	}

	got := string(out)
	for _, want := range []string{
		"marker-begin",
		`"access_mode":"public"`,
		"marker-end",
		"smith-user-exists=yes",
		"passwordless-sudo=yes",
		"ufw-active=active",
		"ufw-default-deny=deny",
		"ufw-ssh-allow=allow",
		"permit-root-login=no",
		"password-authentication=no",
		"fail2ban=running",
		"auto-updates=enabled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("probe output missing %q:\n%s", want, got)
		}
	}
}

// TestScriptProbeMarksRootFactsUnknownWithoutSudo runs the probe as a non-root
// user whose passwordless sudo is denied: it must report the lost sudo and emit
// '?' for every root-only fact rather than guessing them.
func TestScriptProbeMarksRootFactsUnknownWithoutSudo(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "bootstrap.sh")
	if err := os.WriteFile(scriptPath, []byte(Script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeProbeFakeBins(t, binDir)
	// Fake sudo denies passwordless sudo (`sudo -n` fails), standing in for a
	// smith user that has lost it.
	writeFakeBin(t, binDir, "sudo", `#!/usr/bin/env bash
exit 1
`)

	markerPath := filepath.Join(dir, "bootstrap.json")
	if err := os.WriteFile(markerPath, []byte(`{"schema_version":1,"access_mode":"public","completed_phases":["packages"]}`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	env := append(os.Environ()[:0:0],
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SMITH_MARKER="+markerPath,
	)
	cmd := exec.Command(bash, scriptPath, "probe")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe run failed: %v\n%s", err, out)
	}

	got := string(out)
	if !strings.Contains(got, "passwordless-sudo=no") {
		t.Errorf("probe should report lost sudo:\n%s", got)
	}
	for _, want := range []string{"ufw-active=?", "permit-root-login=?", "fail2ban=?", "auto-updates=?"} {
		if !strings.Contains(got, want) {
			t.Errorf("root-only fact should be undeterminable without sudo, missing %q:\n%s", want, got)
		}
	}
}

// writeProbeFakeBins installs read-only fakes for a provisioned box: sudo execs
// through, getent reports smith present, ufw is active with SSH allowed, sshd -T
// reports the hardened effective config, and systemctl reports fail2ban running.
func writeProbeFakeBins(t *testing.T, binDir string) {
	t.Helper()
	writeFakeBin(t, binDir, "sudo", `#!/usr/bin/env bash
while [ "$1" = "-n" ] || [ "$1" = "-E" ]; do shift; done
exec "$@"
`)
	writeFakeBin(t, binDir, "getent", `#!/usr/bin/env bash
if [ "$1" = "passwd" ] && [ "$2" = "smith" ]; then
  echo "smith:x:1001:1001::/home/smith:/bin/bash"
  exit 0
fi
exit 2
`)
	writeFakeBin(t, binDir, "ufw", `#!/usr/bin/env bash
if [ "$1" = "status" ]; then
  echo "Status: active"
  echo "Default: deny (incoming), allow (outgoing), disabled (routed)"
  echo "22/tcp                     ALLOW       Anywhere"
fi
exit 0
`)
	writeFakeBin(t, binDir, "sshd", `#!/usr/bin/env bash
if [ "$1" = "-T" ]; then
  echo "permitrootlogin no"
  echo "passwordauthentication no"
fi
exit 0
`)
	writeFakeBin(t, binDir, "systemctl", `#!/usr/bin/env bash
case "$1" in
  is-active) exit 0 ;;
esac
exit 0
`)
}
