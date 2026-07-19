package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byran/smith/internal/marker"
)

// TestScriptSetupWritesMarkerAndIsIdempotent runs the embedded bootstrap.sh end
// to end against fake privileged binaries and a temp marker path. It proves the
// phase loop writes the marker, the base-layer phases (packages, smith-user,
// smith-keys) each report their result and record themselves in order, and a
// re-run on an already-provisioned box is a no-op that reports
// already-satisfied — decoding the on-box marker with the marker package to
// confirm the two sides agree on the schema.
func TestScriptSetupWritesMarkerAndIsIdempotent(t *testing.T) {
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
	// Fake sudo drops its leading options and execs the rest, so as_root works
	// without real privilege.
	writeFakeBin(t, binDir, "sudo", `#!/usr/bin/env bash
while [ "$1" = "-n" ] || [ "$1" = "-E" ]; do shift; done
exec "$@"
`)
	// Fake apt-get: `update` is a no-op; `install` prints an apt-style summary
	// whose "newly installed" count is non-zero the first time and zero once the
	// APT_STATE stamp exists, mimicking a box that becomes provisioned.
	writeFakeBin(t, binDir, "apt-get", `#!/usr/bin/env bash
if [ "$1" = "update" ]; then
  echo "Reading package lists... Done"
  exit 0
fi
# Record the install invocation so the test can assert the package scope
# (e.g. that public mode never installs a tailscale package).
printf '%s\n' "$*" >>"$APT_INSTALL_LOG"
if [ -f "$APT_STATE" ]; then
  echo "fail2ban is already the newest version (1.0)."
  echo "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded."
else
  echo "Setting up fail2ban (1.0) ..."
  echo "0 upgraded, 3 newly installed, 0 to remove and 0 not upgraded."
  touch "$APT_STATE"
fi
exit 0
`)
	// Fake getent: reports the smith user as absent until useradd stamps
	// USER_STATE, so the smith-user phase's check-before-change sees the box
	// transition from "no smith user" to "smith user present".
	writeFakeBin(t, binDir, "getent", `#!/usr/bin/env bash
if [ "$1" = "passwd" ] && [ "$2" = "smith" ]; then
  if [ -f "$USER_STATE" ]; then
    echo "smith:x:1001:1001::${SMITH_HOME}:/bin/bash"
    exit 0
  fi
  exit 2
fi
exit 2
`)
	// Fake useradd stamps USER_STATE so the next getent reports the user present.
	writeFakeBin(t, binDir, "useradd", `#!/usr/bin/env bash
touch "$USER_STATE"
exit 0
`)
	// Fake visudo accepts any syntax check, standing in for the real validator.
	writeFakeBin(t, binDir, "visudo", `#!/usr/bin/env bash
exit 0
`)
	// Fake chown is a no-op: the test user cannot chown to smith, and ownership
	// is not what this behavior test asserts.
	writeFakeBin(t, binDir, "chown", `#!/usr/bin/env bash
exit 0
`)
	// Fake ufw tracks a single bit of state (UFW_STATE = enabled): `status
	// verbose` reports inactive until `enable` stamps the state, after which it
	// reports the active, default-deny-inbound, SSH-allowed configuration the
	// firewall phase checks for. This lets the phase transition from "inactive"
	// to "already-satisfied" across the two runs.
	writeFakeBin(t, binDir, "ufw", `#!/usr/bin/env bash
args=()
for a in "$@"; do [ "$a" = "--force" ] || args+=("$a"); done
set -- ${args[@]+"${args[@]}"}
if [ "$1" = "status" ]; then
  if [ -f "$UFW_STATE" ]; then
    echo "Status: active"
    echo "Default: deny (incoming), allow (outgoing), disabled (routed)"
    echo "22/tcp                     ALLOW       Anywhere"
  else
    echo "Status: inactive"
  fi
  exit 0
fi
if [ "$1" = "enable" ]; then
  touch "$UFW_STATE"
fi
exit 0
`)
	// Fake systemctl tracks fail2ban's enabled+running state (FAIL2BAN_STATE):
	// is-active/is-enabled fail until `enable --now` stamps the state, so the
	// fail2ban phase transitions from starting the service to already-satisfied.
	writeFakeBin(t, binDir, "systemctl", `#!/usr/bin/env bash
case "$1" in
  is-active|is-enabled)
    [ -f "$FAIL2BAN_STATE" ] && exit 0 || exit 3
    ;;
  enable)
    touch "$FAIL2BAN_STATE"
    ;;
esac
exit 0
`)
	// Fake unattended-upgrade records each invocation so the test can assert the
	// one-time end-of-run patch fires exactly once (on the clean run) and never
	// on the already-satisfied re-run.
	writeFakeBin(t, binDir, "unattended-upgrade", `#!/usr/bin/env bash
echo "ran" >>"$UU_LOG"
exit 0
`)

	markerPath := filepath.Join(dir, "bootstrap.json")
	aptState := filepath.Join(dir, "apt.installed")
	aptInstallLog := filepath.Join(dir, "apt.install.log")
	userState := filepath.Join(dir, "user.created")
	sudoersDir := filepath.Join(dir, "sudoers.d")
	smithHome := filepath.Join(dir, "smith-home")
	ufwState := filepath.Join(dir, "ufw.enabled")
	fail2banState := filepath.Join(dir, "fail2ban.running")
	autoUpgradesConf := filepath.Join(dir, "20auto-upgrades")
	uuLog := filepath.Join(dir, "unattended-upgrade.log")
	// The bootstrap login's home, holding the authorized_keys smith-keys copies.
	loginHome := filepath.Join(dir, "login-home")
	authKeys := "ssh-ed25519 AAAAC3NzaC1lZDI1 operator@laptop\n"
	if err := os.MkdirAll(filepath.Join(loginHome, ".ssh"), 0o700); err != nil {
		t.Fatalf("mkdir login .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(loginHome, ".ssh", "authorized_keys"), []byte(authKeys), 0o600); err != nil {
		t.Fatalf("write login authorized_keys: %v", err)
	}

	run := func() (string, error) {
		cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
		cmd.Env = append(os.Environ(),
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"HOME="+loginHome,
			"SMITH_MARKER="+markerPath,
			"SMITH_HOME="+smithHome,
			"SMITH_SUDOERS_DIR="+sudoersDir,
			"SMITH_AUTO_UPGRADES_CONF="+autoUpgradesConf,
			"APT_STATE="+aptState,
			"APT_INSTALL_LOG="+aptInstallLog,
			"USER_STATE="+userState,
			"UFW_STATE="+ufwState,
			"FAIL2BAN_STATE="+fail2banState,
			"UU_LOG="+uuLog,
		)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// First run: the base layer provisions the box, so every phase reports
	// changed, not already-satisfied.
	out1, err := run()
	if err != nil {
		t.Fatalf("first setup run failed: %v\n%s", err, out1)
	}
	for _, phase := range []string{"▶ packages", "▶ smith-user", "▶ smith-keys", "▶ firewall", "▶ fail2ban", "▶ auto-updates"} {
		if !strings.Contains(out1, phase) {
			t.Errorf("first run missing live phase progress %q:\n%s", phase, out1)
		}
	}
	if strings.Contains(out1, "already-satisfied") {
		t.Errorf("first run should not be already-satisfied:\n%s", out1)
	}

	// The marker the script wrote must decode with the marker package (the two
	// sides agree on schema), record the base-layer phases in order, and carry
	// the run's access mode and smith version.
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	m, skew, err := marker.Decode(data)
	if err != nil {
		t.Fatalf("decode marker written by bootstrap.sh: %v\n%s", err, data)
	}
	if skew != marker.SkewNone {
		t.Errorf("marker skew = %v, want SkewNone (schema written = %d)", skew, m.SchemaVersion)
	}
	if m.AccessMode != "public" {
		t.Errorf("marker AccessMode = %q, want %q", m.AccessMode, "public")
	}
	if m.SmithVersion != "9.9.9-test" {
		t.Errorf("marker SmithVersion = %q, want %q", m.SmithVersion, "9.9.9-test")
	}
	if strings.Join(m.CompletedPhases, ",") != "packages,smith-user,smith-keys,firewall,fail2ban,auto-updates" {
		t.Errorf("marker CompletedPhases = %v, want [packages smith-user smith-keys firewall fail2ban auto-updates]", m.CompletedPhases)
	}

	// smith-user must have laid down the passwordless-sudo drop-in.
	sudoers, err := os.ReadFile(filepath.Join(sudoersDir, "smith"))
	if err != nil {
		t.Fatalf("read smith sudoers drop-in: %v", err)
	}
	if strings.TrimSpace(string(sudoers)) != "smith ALL=(ALL) NOPASSWD:ALL" {
		t.Errorf("smith sudoers drop-in = %q, want passwordless-sudo grant", string(sudoers))
	}

	// smith-keys must have copied the bootstrap login's authorized_keys onto
	// smith verbatim.
	gotKeys, err := os.ReadFile(filepath.Join(smithHome, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read smith authorized_keys: %v", err)
	}
	if string(gotKeys) != authKeys {
		t.Errorf("smith authorized_keys = %q, want %q", string(gotKeys), authKeys)
	}

	// firewall must have activated ufw (default-deny inbound with SSH open on 22
	// for the public connect door).
	if _, err := os.Stat(ufwState); err != nil {
		t.Errorf("firewall phase did not activate ufw: %v", err)
	}
	// fail2ban must have been enabled and started.
	if _, err := os.Stat(fail2banState); err != nil {
		t.Errorf("fail2ban phase did not enable/start the service: %v", err)
	}
	// auto-updates must have written the unattended security-upgrades config.
	conf, err := os.ReadFile(autoUpgradesConf)
	if err != nil {
		t.Fatalf("read auto-upgrades config: %v", err)
	}
	for _, want := range []string{
		`APT::Periodic::Update-Package-Lists "1";`,
		`APT::Periodic::Unattended-Upgrade "1";`,
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("auto-upgrades config missing %q:\n%s", want, conf)
		}
	}
	// The one-time end-of-run patch must have fired exactly once on the clean run.
	uu, err := os.ReadFile(uuLog)
	if err != nil {
		t.Fatalf("read unattended-upgrade log: %v", err)
	}
	if got := strings.Count(string(uu), "ran"); got != 1 {
		t.Errorf("unattended-upgrade ran %d times on the clean run, want 1", got)
	}
	// Public mode installs no tailscale package: the recorded apt install
	// invocations must never mention tailscale, and no blanket apt upgrade runs.
	installLog, err := os.ReadFile(aptInstallLog)
	if err != nil {
		t.Fatalf("read apt install log: %v", err)
	}
	if strings.Contains(string(installLog), "tailscale") {
		t.Errorf("public mode installed a tailscale package:\n%s", installLog)
	}

	// Second run: nothing to change, so every phase reports already-satisfied
	// and the marker still lists exactly the base-layer phases in order.
	out2, err := run()
	if err != nil {
		t.Fatalf("second setup run failed: %v\n%s", err, out2)
	}
	for _, phase := range []string{
		"✓ packages (already-satisfied)",
		"✓ smith-user (already-satisfied)",
		"✓ smith-keys (already-satisfied)",
		"✓ firewall (already-satisfied)",
		"✓ fail2ban (already-satisfied)",
		"✓ auto-updates (already-satisfied)",
	} {
		if !strings.Contains(out2, phase) {
			t.Errorf("re-run should report %q:\n%s", phase, out2)
		}
	}
	// The one-time patch must not fire again on the already-satisfied re-run.
	uu2, err := os.ReadFile(uuLog)
	if err != nil {
		t.Fatalf("read unattended-upgrade log after re-run: %v", err)
	}
	if got := strings.Count(string(uu2), "ran"); got != 1 {
		t.Errorf("unattended-upgrade ran %d times total after re-run, want 1 (no re-patch)", got)
	}

	data2, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker after re-run: %v", err)
	}
	m2, _, err := marker.Decode(data2)
	if err != nil {
		t.Fatalf("decode marker after re-run: %v", err)
	}
	if strings.Join(m2.CompletedPhases, ",") != "packages,smith-user,smith-keys,firewall,fail2ban,auto-updates" {
		t.Errorf("re-run marker CompletedPhases = %v, want [packages smith-user smith-keys firewall fail2ban auto-updates]", m2.CompletedPhases)
	}
}

// writeFakeBin writes an executable stub script into dir under name.
func writeFakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}
