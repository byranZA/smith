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
// to end against fake apt-get/sudo binaries and a temp marker path. It proves
// the phase loop writes the marker, the packages phase reports its result, and
// a re-run on an already-provisioned box is a no-op that reports
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

	markerPath := filepath.Join(dir, "bootstrap.json")
	aptState := filepath.Join(dir, "apt.installed")
	run := func() (string, error) {
		cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
		cmd.Env = append(os.Environ(),
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"SMITH_MARKER="+markerPath,
			"APT_STATE="+aptState,
		)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// First run: packages get installed, so the phase reports changed, not
	// already-satisfied.
	out1, err := run()
	if err != nil {
		t.Fatalf("first setup run failed: %v\n%s", err, out1)
	}
	if !strings.Contains(out1, "▶ packages") {
		t.Errorf("first run missing live phase progress:\n%s", out1)
	}
	if strings.Contains(out1, "already-satisfied") {
		t.Errorf("first run should not be already-satisfied:\n%s", out1)
	}

	// The marker the script wrote must decode with the marker package (the two
	// sides agree on schema), record the packages phase, and carry the run's
	// access mode and smith version.
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
	if strings.Join(m.CompletedPhases, ",") != "packages" {
		t.Errorf("marker CompletedPhases = %v, want [packages]", m.CompletedPhases)
	}

	// Second run: nothing to install, so the packages phase reports
	// already-satisfied and the marker still lists exactly the packages phase.
	out2, err := run()
	if err != nil {
		t.Fatalf("second setup run failed: %v\n%s", err, out2)
	}
	if !strings.Contains(out2, "already-satisfied") {
		t.Errorf("re-run should report already-satisfied:\n%s", out2)
	}

	data2, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker after re-run: %v", err)
	}
	m2, _, err := marker.Decode(data2)
	if err != nil {
		t.Fatalf("decode marker after re-run: %v", err)
	}
	if strings.Join(m2.CompletedPhases, ",") != "packages" {
		t.Errorf("re-run marker CompletedPhases = %v, want [packages]", m2.CompletedPhases)
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
