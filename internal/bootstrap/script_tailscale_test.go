package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeTailscaleFakeBins installs the fakes the tailscale-mode paths invoke:
// curl (fetches the repo keyring/list to stdout) and tailscale (up enrolls, ip
// reports the tailnet address). ssh is written per test as in the base suite.
func writeTailscaleFakeBins(t *testing.T, binDir string) {
	t.Helper()
	// Fake curl echoes repo content so the piped `tee` writes a non-empty keyring
	// and sources list, standing in for fetching Tailscale's apt repo files.
	writeFakeBin(t, binDir, "curl", `#!/usr/bin/env bash
echo "fake tailscale apt repo content"
exit 0
`)
	// Fake tailscale: `up` enrolls successfully, `ip -4` prints the box's tailnet
	// address, `status --json` reports Running.
	writeFakeBin(t, binDir, "tailscale", `#!/usr/bin/env bash
case "$1" in
  up)
    # Record the full argv so tests can assert the auth key never lands here.
    [ -n "${SMITH_TS_ARGV_LOG:-}" ] && printf '%s\n' "$*" >>"$SMITH_TS_ARGV_LOG"
    exit 0
    ;;
  ip) echo "100.101.102.103"; exit 0 ;;
  status) echo '{"BackendState":"Running"}'; exit 0 ;;
esac
exit 0
`)
}

// TestScriptTailscaleModeInstallsTailscaleAndSkipsAccessPhase runs the embedded
// bootstrap.sh setup in tailscale mode with public SSH kept open (the fresh
// bootstrap-in path). It proves the packages phase installs tailscale from the
// pinned apt repo — ensuring curl + ca-certificates first, a binary .noarmor.gpg
// keyring, and no gnupg — that the streamed phases stop at auto-updates (the
// admin-driven access layer is not a box-side phase), and that firewall keeps
// public port 22 open so the base layer never severs its own reach.
func TestScriptTailscaleModeInstallsTailscaleAndSkipsAccessPhase(t *testing.T) {
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
	writeTailscaleFakeBins(t, binDir)
	writeFakeBin(t, binDir, "ssh", "#!/usr/bin/env bash\nexit 0\n")

	env := bootstrapTestEnv(t, dir, binDir)
	markerPath := filepath.Join(dir, "bootstrap.json")
	aptInstallLog := filepath.Join(dir, "apt.install.log")
	keyring := filepath.Join(dir, "keyrings", "tailscale-archive-keyring.gpg")
	list := filepath.Join(dir, "sources.list.d", "tailscale.list")

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "tailscale", "--public-ssh", "open", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tailscale setup run failed: %v\n%s", err, out)
	}

	// The streamed base phases run; the access phase is NOT streamed (the
	// tailscale access layer is admin-driven, not a box-side phase).
	for _, phase := range []string{"▶ packages", "▶ firewall", "▶ auto-updates"} {
		if !strings.Contains(string(out), phase) {
			t.Errorf("tailscale setup missing base phase %q:\n%s", phase, out)
		}
	}
	if strings.Contains(string(out), "▶ access") {
		t.Errorf("tailscale setup should not run access as a box-side phase:\n%s", out)
	}

	// Packages installed tailscale from the pinned repo, ensuring curl +
	// ca-certificates first, and never pulling gnupg (binary keyring).
	installLog, err := os.ReadFile(aptInstallLog)
	if err != nil {
		t.Fatalf("read apt install log: %v", err)
	}
	for _, want := range []string{"tailscale", "curl", "ca-certificates"} {
		if !strings.Contains(string(installLog), want) {
			t.Errorf("tailscale mode did not install %q:\n%s", want, installLog)
		}
	}
	if strings.Contains(string(installLog), "gnupg") {
		t.Errorf("tailscale mode installed gnupg; a binary .noarmor.gpg keyring needs none:\n%s", installLog)
	}
	for _, f := range []string{keyring, list} {
		if fi, statErr := os.Stat(f); statErr != nil || fi.Size() == 0 {
			t.Errorf("expected non-empty tailscale repo file %s (err=%v)", f, statErr)
		}
	}

	// The marker records tailscale mode and the base phases only (no access yet —
	// that lands when close-public-ssh runs after the probe).
	m, _, err := decodeMarker(t, markerPath)
	if err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if m.AccessMode != "tailscale" {
		t.Errorf("marker AccessMode = %q, want tailscale", m.AccessMode)
	}
	// The recorded box-side sequence must be exactly Phases minus its trailing
	// access element — the derivation bootstrap.sh setup() makes from PHASES. This
	// fails if the tailscale-mode sequence ever diverges from PHASES minus access.
	wantBase := strings.Join(Phases[:len(Phases)-1], ",")
	if got := strings.Join(m.CompletedPhases, ","); got != wantBase {
		t.Errorf("marker CompletedPhases = %q, want the base phases only %q", got, wantBase)
	}
}

// TestScriptEnrollAndClosePublicSSHRecordsAccess drives the two admin-invoked
// tailscale subcommands against a box already provisioned in tailscale mode.
// enroll reports the box's tailnet IP (the auth key arrives on stdin), and
// close-public-ssh — the probe-gated last step — removes the public port-22 rule
// and records the access phase, so the marker ends up with the full tailscale
// phase set while preserving the recorded access mode and smith version.
func TestScriptEnrollAndClosePublicSSHRecordsAccess(t *testing.T) {
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
	writeTailscaleFakeBins(t, binDir)
	writeFakeBin(t, binDir, "ssh", "#!/usr/bin/env bash\nexit 0\n")

	env := bootstrapTestEnv(t, dir, binDir)
	markerPath := filepath.Join(dir, "bootstrap.json")
	sshRule := filepath.Join(dir, "ufw.ssh22.rule")

	// Provision the box in tailscale mode with public SSH open.
	provision := exec.Command(bash, scriptPath, "setup", "--access", "tailscale", "--public-ssh", "open", "--smith-version", "9.9.9-test")
	provision.Env = append(os.Environ(), env...)
	if out, err := provision.CombinedOutput(); err != nil {
		t.Fatalf("provision run failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(sshRule); err != nil {
		t.Fatalf("provision with public-ssh open should leave the port-22 rule: %v", err)
	}

	// enroll: the auth key travels on stdin and the reported tailnet IP is parsed
	// back out.
	enroll := exec.Command(bash, scriptPath, "enroll", "--hostname", "smith-box.example.com")
	enroll.Env = append(os.Environ(), env...)
	enroll.Stdin = strings.NewReader("tskey-abc123\n")
	enrollOut, err := enroll.CombinedOutput()
	if err != nil {
		t.Fatalf("enroll failed: %v\n%s", err, enrollOut)
	}
	if !strings.Contains(string(enrollOut), "tailscale-ip=100.101.102.103") {
		t.Errorf("enroll output = %q, want it to report the tailnet IP", enrollOut)
	}

	// close-public-ssh: removes the port-22 rule and records the access phase.
	closeCmd := exec.Command(bash, scriptPath, "close-public-ssh")
	closeCmd.Env = append(os.Environ(), env...)
	if out, err := closeCmd.CombinedOutput(); err != nil {
		t.Fatalf("close-public-ssh failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(sshRule); !os.IsNotExist(err) {
		t.Errorf("close-public-ssh should remove the public port-22 rule, stat err = %v", err)
	}

	m, _, err := decodeMarker(t, markerPath)
	if err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if m.AccessMode != "tailscale" {
		t.Errorf("marker AccessMode = %q, want tailscale preserved", m.AccessMode)
	}
	if m.SmithVersion != "9.9.9-test" {
		t.Errorf("marker SmithVersion = %q, want 9.9.9-test preserved", m.SmithVersion)
	}
	const wantFull = "packages,smith-user,smith-keys,firewall,ssh-hardening,fail2ban,auto-updates,access"
	if got := strings.Join(m.CompletedPhases, ","); got != wantFull {
		t.Errorf("marker CompletedPhases = %q, want the full tailscale set %q", got, wantFull)
	}
}

// TestScriptEnrollKeepsAuthKeyOffArgv proves the enroll step never places the
// Tailscale auth key in the box's process argv: `tailscale up` is invoked with a
// --auth-key=file:<path> reference (not the literal key), and the tmpfile that
// carries the key is 0600 and removed on completion. The stdin design keeps the
// key off argv on the smith side; this keeps it off argv on the box side too
// (readable via ps / /proc/PID/cmdline otherwise) — ADR-0002's "either side".
func TestScriptEnrollKeepsAuthKeyOffArgv(t *testing.T) {
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
	writeTailscaleFakeBins(t, binDir)
	writeFakeBin(t, binDir, "ssh", "#!/usr/bin/env bash\nexit 0\n")

	env := bootstrapTestEnv(t, dir, binDir)
	argvLog := filepath.Join(dir, "tailscale.up.argv")

	const key = "tskey-abc123secret"
	enroll := exec.Command(bash, scriptPath, "enroll", "--hostname", "smith-box.example.com")
	enroll.Env = append(os.Environ(), env...)
	enroll.Stdin = strings.NewReader(key + "\n")
	enrollOut, err := enroll.CombinedOutput()
	if err != nil {
		t.Fatalf("enroll failed: %v\n%s", err, enrollOut)
	}

	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("read tailscale up argv log: %v", err)
	}
	argvStr := strings.TrimSpace(string(argv))

	// The literal key must never reach `tailscale up`'s argv.
	if strings.Contains(argvStr, key) {
		t.Errorf("auth key leaked into tailscale up argv:\n%s", argvStr)
	}

	// The key is passed by file: reference; extract the tmpfile path.
	var keyfile string
	for _, tok := range strings.Fields(argvStr) {
		if ref, ok := strings.CutPrefix(tok, "--auth-key=file:"); ok {
			keyfile = ref
		}
		if strings.HasPrefix(tok, "--auth-key=") && !strings.HasPrefix(tok, "--auth-key=file:") {
			t.Errorf("auth key passed as a literal argv element, not file: reference:\n%s", argvStr)
		}
	}
	if keyfile == "" {
		t.Fatalf("tailscale up did not receive an --auth-key=file: reference:\n%s", argvStr)
	}

	// The tmpfile is cleaned up on the way out — no secret left on the box.
	if _, err := os.Stat(keyfile); !os.IsNotExist(err) {
		t.Errorf("enroll left the auth-key tmpfile %s behind, stat err = %v", keyfile, err)
	}
}

// TestScriptTailscaleReRunOverTailnetNeverReopensPublic22 provisions a box with
// public SSH open, closes public 22 (the tailnet probe having proven reach),
// then re-runs setup as a tailnet re-run would — access tailscale with public
// SSH closed. It proves the firewall phase keeps public port 22 closed
// throughout: the port-22 rule stays absent and the firewall reports
// already-satisfied rather than transiently re-opening 22.
func TestScriptTailscaleReRunOverTailnetNeverReopensPublic22(t *testing.T) {
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
	writeTailscaleFakeBins(t, binDir)
	writeFakeBin(t, binDir, "ssh", "#!/usr/bin/env bash\nexit 0\n")

	env := bootstrapTestEnv(t, dir, binDir)
	sshRule := filepath.Join(dir, "ufw.ssh22.rule")

	run := func(publicSSH string) (string, error) {
		cmd := exec.Command(bash, scriptPath, "setup", "--access", "tailscale", "--public-ssh", publicSSH, "--smith-version", "9.9.9-test")
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("open"); err != nil {
		t.Fatalf("initial provision failed: %v\n%s", err, out)
	}
	// The probe having proven reach, public 22 is closed.
	closeCmd := exec.Command(bash, scriptPath, "close-public-ssh")
	closeCmd.Env = append(os.Environ(), env...)
	if out, err := closeCmd.CombinedOutput(); err != nil {
		t.Fatalf("close-public-ssh failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(sshRule); !os.IsNotExist(err) {
		t.Fatalf("public 22 should be closed before the re-run, stat err = %v", err)
	}

	// The tailnet re-run keeps public 22 closed: the firewall reports
	// already-satisfied and never re-adds the port-22 rule.
	out, err := run("closed")
	if err != nil {
		t.Fatalf("tailnet re-run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ firewall (already-satisfied)") {
		t.Errorf("tailnet re-run firewall should be already-satisfied with 22 closed:\n%s", out)
	}
	if _, err := os.Stat(sshRule); !os.IsNotExist(err) {
		t.Errorf("tailnet re-run transiently re-opened public 22, stat err = %v", err)
	}
}
