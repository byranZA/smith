package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// probingSSH stands in for the local ssh binary during a probe run: it answers
// for every target except the ones named unreachable, and records what it was
// pointed at. Probes run concurrently, so it guards its own records.
type probingSSH struct {
	unreachable map[string]bool

	mu      sync.Mutex
	targets []string
}

// Run records the ssh destination and refuses the connection for a target the
// fake was told does not answer.
func (s *probingSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	if name != "ssh" || len(args) < 2 {
		return nil
	}
	target := args[len(args)-2]
	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.mu.Unlock()
	if s.unreachable[target] {
		return refusedExit{}
	}
	return nil
}

// runProbingList runs `machine list --probe` against a config home rooted at
// dir, with every ssh launch pointed at the given fake.
func runProbingList(t *testing.T, dir string, ssh *probingSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, idleDialer{t: t}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"list"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// twoBoxInventory registers the two boxes every probing case is decided against.
const twoBoxInventory = `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"},"scratch":{"target":"smith@203.0.113.42"}}}`

func TestMachineListProbeFlagsAnUnreachableBoxInline(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, twoBoxInventory)
	ssh := &probingSSH{unreachable: map[string]bool{"smith@203.0.113.42": true}}

	stdout, stderr, code := runProbingList(t, dir, ssh, "--probe")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a listing with an unreachable box (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "REACHABLE") {
		t.Errorf("stdout = %q, want a REACHABLE column", stdout)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if !strings.HasPrefix(lines[1], "dev") || !strings.HasSuffix(lines[1], "reachable") || strings.HasSuffix(lines[1], "unreachable") {
		t.Errorf("line for dev = %q, want it shown as reachable", lines[1])
	}
	if !strings.HasPrefix(lines[2], "scratch") || !strings.HasSuffix(lines[2], "unreachable") {
		t.Errorf("line for scratch = %q, want it shown as unreachable", lines[2])
	}
	if len(ssh.targets) != 2 {
		t.Errorf("ssh targets = %v, want one probe per box", ssh.targets)
	}
}

func TestMachineListProbeLeavesTheInventoryUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, twoBoxInventory)
	path := config.NewHome(dir).InventoryPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	ssh := &probingSSH{unreachable: map[string]bool{"smith@203.0.113.42": true}}

	stdout, _, _ := runProbingList(t, dir, ssh, "--probe")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("inventory = %q, want it byte-identical after a probe run (was %q)", after, before)
	}
	if !strings.Contains(stdout, "scratch") {
		t.Errorf("stdout = %q, want the unreachable box still listed, never pruned", stdout)
	}
}
