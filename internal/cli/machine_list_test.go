package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/provider"
)

// idleRunner and idleDialer stand in for the two ways a command could reach a
// box. Listing is offline, so any use of either is the failure.

// idleRunner fails the test if a command launches a provider CLI.
type idleRunner struct{ t *testing.T }

// Run records the failure: listing runs no command anywhere.
func (r idleRunner) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	r.t.Helper()
	r.t.Errorf("machine list ran %s %v, want it to run no command", name, args)
	return nil
}

// idleDialer fails the test if a command opens a connection.
type idleDialer struct{ t *testing.T }

// Dial records the failure: listing opens no connection.
func (d idleDialer) Dial(_ context.Context, address string) error {
	d.t.Helper()
	d.t.Errorf("machine list dialled %s, want it to open no connection", address)
	return nil
}

// runList runs `machine list` against a config home rooted at dir and returns
// what landed on each stream plus the process exit code. It is driven through
// the real machine command tree, whose only ways out to a box are the runner
// and dialer given here — and both fail the test if used.
func runList(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		idleRunner{t: t}, idleDialer{t: t}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"list"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// writeInventory puts an inventory file inside the config home rooted at dir.
func writeInventory(t *testing.T, dir, content string) {
	t.Helper()
	home := config.NewHome(dir)
	if err := os.MkdirAll(home.CachePath(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(home.InventoryPath(), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestMachineListPrintsRegisteredBoxesInNameOrder(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"scratch":{"target":"smith@203.0.113.42"},"dev":{"target":"smith@100.92.14.7"}}}`)

	stdout, stderr, code := runList(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith@100.92.14.7") || !strings.Contains(stdout, "smith@203.0.113.42") {
		t.Errorf("stdout = %q, want both boxes listed with their targets", stdout)
	}
	if strings.Index(stdout, "dev") > strings.Index(stdout, "scratch") {
		t.Errorf("stdout = %q, want the boxes in name order", stdout)
	}
	if !strings.Contains(stdout, "2 boxes") {
		t.Errorf("stdout = %q, want a summary line reporting 2 boxes", stdout)
	}
}

func TestMachineListReportsAnEmptyInventory(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{}}`)

	stdout, stderr, code := runList(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for an empty inventory (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "no boxes registered") || !strings.Contains(stdout, "smith machine add") {
		t.Errorf("stdout = %q, want it to say nothing is registered and name the command that registers one", stdout)
	}
}

func TestMachineListCreatesNothingWhenThereIsNoConfigHome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")

	stdout, stderr, code := runList(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 with no config home (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "no boxes registered") {
		t.Errorf("stdout = %q, want it to report that no boxes are registered", stdout)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) err = %v, want listing to have created nothing", dir, err)
	}
}

func TestMachineListRefusesAMalformedInventory(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, "{not json")

	stdout, stderr, code := runList(t, dir)

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for an unreadable inventory")
	}
	if !strings.Contains(stderr, config.NewHome(dir).InventoryPath()) {
		t.Errorf("stderr = %q, want it to name the inventory's path", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing listed from a file smith could not read", stdout)
	}
}

func TestMachineListNotesAnInventoryANewerSmithWrote(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	stdout, stderr, code := runList(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("stdout = %q, want the entries smith recognises listed", stdout)
	}
	if !strings.Contains(stdout+stderr, "newer smith") {
		t.Errorf("output = %q, want a note that a newer smith wrote the file", stdout+stderr)
	}
}
