package bootstrap

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScriptSetupAbortsWithoutReportingAPhaseItCannotRecord(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	dir, scriptPath, env := markerDirFixture(t)
	markerDir := filepath.Join(dir, "etc-smith")
	snapshot := filepath.Join(dir, "marker.before-swap")
	writeFakeBin(t, filepath.Join(dir, "bin"), "mkswap", `#!/usr/bin/env bash
cp "$SMITH_MARKER" "`+snapshot+`"
chmod 0555 "`+markerDir+`"
`)
	t.Cleanup(func() {
		if err := os.Chmod(markerDir, 0o755); err != nil {
			t.Errorf("restore marker dir permissions: %v", err)
		}
	})

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("setup succeeded though the swap phase could not be recorded:\n%s", out)
	}
	if !strings.Contains(string(out), "▶ swap") {
		t.Fatalf("setup never started the swap phase:\n%s", out)
	}
	if strings.Contains(string(out), "✓ swap") {
		t.Errorf("setup reported swap done though the marker never recorded it:\n%s", out)
	}
	want, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("read marker snapshot: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(markerDir, "bootstrap.json"))
	if err != nil {
		t.Fatalf("read marker after the failed record: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("marker after the failed record = %q, want the previous marker %q", got, want)
	}
}

func TestScriptSetupLeavesOnlyTheMarkerInItsDirectory(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	dir, scriptPath, env := markerDirFixture(t)
	markerDir := filepath.Join(dir, "etc-smith")

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	if _, _, err := decodeMarker(t, filepath.Join(markerDir, "bootstrap.json")); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	entries, err := os.ReadDir(markerDir)
	if err != nil {
		t.Fatalf("read marker dir: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"bootstrap.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("marker dir after setup holds %q, want %q", got, want)
	}
}

func TestScriptSetupKeepsTheMarkerWhenTheCapabilityGuardFails(t *testing.T) {
	t.Parallel()
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
	for _, tool := range []string{"dirname", "basename", "id", "date", "mkdir", "mktemp", "tee", "chmod", "mv", "rm"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not available", tool)
		}
		if err := os.Symlink(real, filepath.Join(binDir, tool)); err != nil {
			t.Fatalf("symlink %s: %v", tool, err)
		}
	}
	writeFakeBin(t, binDir, "apt-get", "#!/usr/bin/env bash\nexit 0\n")
	writeFakeBin(t, binDir, "sudo", "#!"+bash+"\nshift\nexec \"$@\"\n")
	markerPath := filepath.Join(dir, "bootstrap.json")
	want := []byte(`{
  "schema_version": 2,
  "smith_version": "1.0.0",
  "access_mode": "public",
  "completed_phases": ["swap", "packages"],
  "updated_at": "2026-01-02T03:04:05Z"
}
`)
	if err := os.WriteFile(markerPath, want, 0o644); err != nil {
		t.Fatalf("write previous marker: %v", err)
	}

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = []string{"PATH=" + binDir, "SMITH_MARKER=" + markerPath}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("setup succeeded though systemd is missing:\n%s", out)
	}
	if !strings.Contains(string(out), "required capability missing: systemd") {
		t.Fatalf("setup failed for another reason than the capability guard:\n%s", out)
	}
	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker after the failed guard: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("marker after the failed guard = %q, want the previous marker %q", got, want)
	}
}

// markerDirFixture is scriptFixture with the marker in a directory of its own, dir/etc-smith, as on a real box.
func markerDirFixture(t *testing.T) (dir, scriptPath string, env []string) {
	t.Helper()
	dir, scriptPath, env = scriptFixture(t)
	markerDir := filepath.Join(dir, "etc-smith")
	if err := os.Mkdir(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	return dir, scriptPath, append(env, "SMITH_MARKER="+filepath.Join(markerDir, "bootstrap.json"))
}
