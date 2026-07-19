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
// phase loop writes the marker, the base-layer phases each report their result
// and record themselves in order — through the ssh-hardening self-test and the
// terminal public-mode access no-op — and a re-run on an already-provisioned box
// is a no-op that reports already-satisfied. It decodes the on-box marker with
// the marker package to confirm the two sides agree on the schema.
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
	writeProvisioningFakeBins(t, binDir)
	// Fake ssh: the loopback login probe `ssh smith@localhost true` succeeds, so
	// the ssh-hardening self-test proves smith can still log in and the hardening
	// stands.
	writeFakeBin(t, binDir, "ssh", `#!/usr/bin/env bash
exit 0
`)

	env := bootstrapTestEnv(t, dir, binDir)
	markerPath := filepath.Join(dir, "bootstrap.json")
	aptInstallLog := filepath.Join(dir, "apt.install.log")
	sudoersDir := filepath.Join(dir, "sudoers.d")
	smithHome := filepath.Join(dir, "smith-home")
	ufwState := filepath.Join(dir, "ufw.enabled")
	fail2banState := filepath.Join(dir, "fail2ban.running")
	autoUpgradesConf := filepath.Join(dir, "20auto-upgrades")
	uuLog := filepath.Join(dir, "unattended-upgrade.log")
	sshdDropin := filepath.Join(dir, "sshd_config.d", "01-smith-hardening.conf")
	authKeys := "ssh-ed25519 AAAAC3NzaC1lZDI1 operator@laptop\n"

	run := func() (string, error) {
		cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// First run: the base layer provisions the box, so every phase reports
	// changed — except the public-mode access phase, which is a terminal no-op.
	out1, err := run()
	if err != nil {
		t.Fatalf("first setup run failed: %v\n%s", err, out1)
	}
	for _, phase := range []string{"▶ packages", "▶ smith-user", "▶ smith-keys", "▶ firewall", "▶ ssh-hardening", "▶ fail2ban", "▶ auto-updates", "▶ access"} {
		if !strings.Contains(out1, phase) {
			t.Errorf("first run missing live phase progress %q:\n%s", phase, out1)
		}
	}
	for _, phase := range []string{"packages", "smith-user", "smith-keys", "firewall", "ssh-hardening", "fail2ban", "auto-updates"} {
		if strings.Contains(out1, "✓ "+phase+" (already-satisfied)") {
			t.Errorf("clean run: phase %q reported already-satisfied, want changed:\n%s", phase, out1)
		}
	}
	// The access phase is a no-op in public mode, so it reports already-satisfied
	// even on a clean run.
	if !strings.Contains(out1, "✓ access (already-satisfied)") {
		t.Errorf("clean run: public-mode access phase should report already-satisfied:\n%s", out1)
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
	const wantPhases = "packages,smith-user,smith-keys,firewall,ssh-hardening,fail2ban,auto-updates,access"
	if got := strings.Join(m.CompletedPhases, ","); got != wantPhases {
		t.Errorf("marker CompletedPhases = %q, want %q", got, wantPhases)
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
	// ssh-hardening must have written the drop-in with PermitRootLogin no and
	// PasswordAuthentication no (full hardening — no prohibit-password softening).
	dropin, err := os.ReadFile(sshdDropin)
	if err != nil {
		t.Fatalf("read ssh-hardening drop-in: %v", err)
	}
	for _, want := range []string{"PermitRootLogin no", "PasswordAuthentication no"} {
		if !strings.Contains(string(dropin), want) {
			t.Errorf("ssh-hardening drop-in missing %q:\n%s", want, dropin)
		}
	}
	if strings.Contains(string(dropin), "prohibit-password") {
		t.Errorf("ssh-hardening drop-in softened to prohibit-password:\n%s", dropin)
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
		"✓ ssh-hardening (already-satisfied)",
		"✓ fail2ban (already-satisfied)",
		"✓ auto-updates (already-satisfied)",
		"✓ access (already-satisfied)",
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
	if got := strings.Join(m2.CompletedPhases, ","); got != wantPhases {
		t.Errorf("re-run marker CompletedPhases = %q, want %q", got, wantPhases)
	}
}

// TestScriptSshHardeningSelfRevertsOnLockout drives the embedded bootstrap.sh
// against a box where the hardened sshd config would lock smith out: the loopback
// probe `ssh smith@localhost true` is denied. It proves the ssh-hardening phase's
// on-box self-test self-reverts — the hardening drop-in is removed, the phase
// exits non-zero without recording ssh-hardening, and the phases that ran before
// it stay recorded — so the box is left partial but reachable as smith and no
// door was closed.
func TestScriptSshHardeningSelfRevertsOnLockout(t *testing.T) {
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
	writeProvisioningFakeBins(t, binDir)
	// Fake ssh: the loopback login probe is denied, standing in for a hardened
	// config that would lock smith out — the trigger for the self-revert.
	writeFakeBin(t, binDir, "ssh", `#!/usr/bin/env bash
echo "smith@localhost: Permission denied (publickey)." >&2
exit 255
`)

	env := bootstrapTestEnv(t, dir, binDir)
	markerPath := filepath.Join(dir, "bootstrap.json")
	dropinPath := filepath.Join(dir, "sshd_config.d", "01-smith-hardening.conf")

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("setup should exit non-zero when the ssh-hardening self-test is locked out, but it succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "▶ ssh-hardening") {
		t.Errorf("self-revert run should reach the ssh-hardening phase:\n%s", out)
	}
	if strings.Contains(string(out), "✓ ssh-hardening") {
		t.Errorf("ssh-hardening should not report success when the self-test fails:\n%s", out)
	}

	// The hardening drop-in must have been reverted (removed) by the self-test, so
	// sshd stays on the working config that lets smith in.
	if _, statErr := os.Stat(dropinPath); !os.IsNotExist(statErr) {
		t.Errorf("ssh-hardening drop-in should be removed after self-revert, stat err = %v", statErr)
	}

	// The marker records the phases up to but not including ssh-hardening: the box
	// is partial but the ssh-hardening door was never left closed, and the
	// terminal access phase never ran.
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	m, _, err := marker.Decode(data)
	if err != nil {
		t.Fatalf("decode marker: %v\n%s", err, data)
	}
	got := strings.Join(m.CompletedPhases, ",")
	if got != "packages,smith-user,smith-keys,firewall" {
		t.Errorf("marker CompletedPhases = %q, want the phases before ssh-hardening (packages,smith-user,smith-keys,firewall)", got)
	}
	for _, unwanted := range []string{"ssh-hardening", "access"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("marker should not record %q after a locked-out self-revert: %v", unwanted, m.CompletedPhases)
		}
	}
}

// bootstrapTestEnv returns the environment that points the embedded script and
// its fake binaries at writable temp paths under dir, so setup runs end to end
// without root. It also seeds the bootstrap login's authorized_keys that the
// smith-keys phase copies onto the smith user.
func bootstrapTestEnv(t *testing.T, dir, binDir string) []string {
	t.Helper()
	loginHome := filepath.Join(dir, "login-home")
	if err := os.MkdirAll(filepath.Join(loginHome, ".ssh"), 0o700); err != nil {
		t.Fatalf("mkdir login .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(loginHome, ".ssh", "authorized_keys"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1 operator@laptop\n"), 0o600); err != nil {
		t.Fatalf("write login authorized_keys: %v", err)
	}
	return append(os.Environ()[:0:0],
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+loginHome,
		"SMITH_MARKER="+filepath.Join(dir, "bootstrap.json"),
		"SMITH_HOME="+filepath.Join(dir, "smith-home"),
		"SMITH_SUDOERS_DIR="+filepath.Join(dir, "sudoers.d"),
		"SMITH_AUTO_UPGRADES_CONF="+filepath.Join(dir, "20auto-upgrades"),
		"SMITH_SSHD_DROPIN="+filepath.Join(dir, "sshd_config.d", "01-smith-hardening.conf"),
		"APT_STATE="+filepath.Join(dir, "apt.installed"),
		"APT_INSTALL_LOG="+filepath.Join(dir, "apt.install.log"),
		"USER_STATE="+filepath.Join(dir, "user.created"),
		"UFW_STATE="+filepath.Join(dir, "ufw.enabled"),
		"FAIL2BAN_STATE="+filepath.Join(dir, "fail2ban.running"),
		"UU_LOG="+filepath.Join(dir, "unattended-upgrade.log"),
	)
}

// writeProvisioningFakeBins installs the fake privileged binaries the bootstrap
// phases invoke, so the embedded script runs without real privilege or a real
// box. Each fake tracks just enough state (via the *_STATE env files
// bootstrapTestEnv sets) for a phase's check-before-change to transition from
// "changed" on a clean run to "already-satisfied" on a re-run. The loopback ssh
// probe is written per test, since its success or denial is what each test drives.
func writeProvisioningFakeBins(t *testing.T, binDir string) {
	t.Helper()
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
	// is not what these behavior tests assert.
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
	// A `reload` (used by ssh-hardening to apply the drop-in) always succeeds.
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
	// Fake sshd: `sshd -t` validates the merged config, which is always well-formed
	// here, so the ssh-hardening self-test's validate step succeeds.
	writeFakeBin(t, binDir, "sshd", `#!/usr/bin/env bash
exit 0
`)
}

// writeFakeBin writes an executable stub script into dir under name.
func writeFakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}
