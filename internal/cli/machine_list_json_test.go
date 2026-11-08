package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineListJSONPrintsTheBoxesInNameOrder(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, twoBoxInventory)

	stdout, stderr, code := runList(t, dir, "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"boxes":[{"name":"dev","target":"smith@100.92.14.7"},{"name":"scratch","target":"smith@203.0.113.42"}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestMachineListJSONOfAnEmptyInventoryIsAnEmptyList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")

	stdout, stderr, code := runList(t, dir, "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := `{"schema_version":1,"boxes":[]}` + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestMachineListJSONProbeReportsReach(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, twoBoxInventory)
	ssh := &probingSSH{unreachable: map[string]bool{"smith@203.0.113.42": true}}

	stdout, stderr, code := runProbingList(t, dir, ssh, "--json", "--probe")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"boxes":[{"name":"dev","target":"smith@100.92.14.7","reachable":true},{"name":"scratch","target":"smith@203.0.113.42","reachable":false}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestMachineListJSONKeepsANewerSmithNoteOffStdout(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	stdout, stderr, code := runList(t, dir, "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := `{"schema_version":1,"boxes":[{"name":"dev","target":"smith@100.92.14.7"}]}` + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "newer smith") {
		t.Errorf("stderr = %q, want the newer-smith note", stderr)
	}
}

func TestMachineListJSONPrintsNothingForAnUnreadableInventory(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, "{not json")

	stdout, _, code := runList(t, dir, "--json")

	if code == 0 || stdout != "" {
		t.Errorf("code = %d, stdout = %q, want a failure with empty stdout", code, stdout)
	}
}
