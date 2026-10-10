package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/provider"
)

// markedBox stands in for the local ssh binary in front of one provisioned box:
// a read of the marker answers with what the box carries, and a write through
// tee replaces it with what arrived on stdin.
type markedBox struct {
	marker string
}

// Run answers the remote command ssh was asked to run on the box.
func (b *markedBox) Run(_ context.Context, name string, args []string, stdin io.Reader, stdout, _ io.Writer) error {
	if name != "ssh" || len(args) == 0 {
		return nil
	}
	cmd := args[len(args)-1]
	switch {
	case strings.Contains(cmd, "tee"):
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		b.marker = string(data)
	case strings.Contains(cmd, "bootstrap.json"):
		if _, err := io.WriteString(stdout, b.marker); err != nil {
			return err
		}
	}
	return nil
}

// runRename runs `machine rename` against a config home rooted at dir,
// reaching the box through the given fake ssh.
func runRename(t *testing.T, dir string, ssh connection.Exec, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"rename"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// namedA is a marker on a box registered as "a".
const namedA = `{
  "schema_version": 2,
  "smith_version": "0.9.1",
  "access_mode": "tailscale",
  "name": "a",
  "blueprint": "go-dev",
  "completed_phases": ["packages", "smith-user"],
  "updated_at": "2026-09-01T08:00:00Z"
}
`

// registeredA is an inventory holding box "a" and one other box.
const registeredA = `{"schema_version":1,"boxes":{"a":{"target":"smith@100.92.14.7"},` +
	`"other":{"target":"smith@100.92.14.8"}}}`

func TestMachineRenameMovesTheEntryAndRenamesTheMarker(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	box := &markedBox{marker: namedA}

	_, stderr, code := runRename(t, dir, box, "a", "b")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	listed, _, _ := runList(t, dir)
	const wantListed = "NAME   TARGET\n" +
		"b      smith@100.92.14.7\n" +
		"other  smith@100.92.14.8\n" +
		"\n2 boxes\n"
	if listed != wantListed {
		t.Errorf("machine list = %q, want %q", listed, wantListed)
	}
	if want := strings.Replace(namedA, `"name": "a"`, `"name": "b"`, 1); box.marker != want {
		t.Errorf("marker = %q, want %q", box.marker, want)
	}
}

func TestMachineRenameReportsTheRenameAndHowToReachTheBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)

	stdout, stderr, code := runRename(t, dir, &markedBox{marker: namedA}, "a", "b")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	const want = `renamed "a" to "b"
registered smith@100.92.14.7 as "b"

Reach it by name from now on:
  smith machine status b
  smith machine setup b
`
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestMachineRenameRefusesANameHeldByAnotherBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	box := &markedBox{marker: namedA}

	stdout, stderr, code := runRename(t, dir, box, "a", "other")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a name another box holds (stdout: %s)", stdout)
	}
	if got := inventoryContent(t, dir); got != registeredA {
		t.Errorf("inventory = %q, want it unchanged at %q", got, registeredA)
	}
	if box.marker != namedA {
		t.Errorf("marker = %q, want it unchanged at %q (stderr: %s)", box.marker, namedA, stderr)
	}
}

func TestMachineRenameRefusesWithoutChangingAnything(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		want     string
	}{
		{"unknown box", "staging", "b", `no box named "staging" is registered`},
		{"literal target", "smith@100.92.14.7", "b", `no box named "smith@100.92.14.7" is registered`},
		{"the current name", "a", "a", `already named "a"`},
		{"an empty name", "a", "", "no box name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeInventory(t, dir, registeredA)
			box := &markedBox{marker: namedA}

			_, stderr, code := runRename(t, dir, box, tt.from, tt.to)

			if code != 1 {
				t.Errorf("rename %q %q exit code = %d, want 1", tt.from, tt.to, code)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("rename %q %q stderr = %q, want it to say %q", tt.from, tt.to, stderr, tt.want)
			}
			if got := inventoryContent(t, dir); got != registeredA {
				t.Errorf("rename %q %q inventory = %q, want it unchanged", tt.from, tt.to, got)
			}
			if box.marker != namedA {
				t.Errorf("rename %q %q marker = %q, want it unchanged", tt.from, tt.to, box.marker)
			}
		})
	}
}

func TestMachineRenameRefusesAnUnreachableBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	ssh := unconnectableSSH{stderr: "ssh: connect to host 100.92.14.7 port 22: Connection timed out\n"}

	_, stderr, code := runRename(t, dir, ssh, "a", "b")

	if code != 3 {
		t.Errorf("exit code = %d, want 3 for a connect failure", code)
	}
	const want = "rename refused: ssh smith@100.92.14.7: could not connect to box: the box did not answer; " +
		"check the address, that sshd is listening on port 22, and the provider's firewall\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if got := inventoryContent(t, dir); got != registeredA {
		t.Errorf("inventory = %q, want it unchanged at %q", got, registeredA)
	}
}

func TestMachineRenameRefusesABoxCarryingNoMarker(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	box := &markedBox{}

	_, stderr, code := runRename(t, dir, box, "a", "b")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a box with no marker to rename")
	}
	if !strings.Contains(stderr, "smith machine setup") {
		t.Errorf("stderr = %q, want it to name machine setup as the fix", stderr)
	}
	if box.marker != "" {
		t.Errorf("marker = %q, want none written", box.marker)
	}
	if got := inventoryContent(t, dir); got != registeredA {
		t.Errorf("inventory = %q, want it unchanged at %q", got, registeredA)
	}
}

func TestMachineRenameNamesTheDisagreementWhenTheInventoryWriteFails(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	cache := config.NewHome(dir).CachePath()
	if err := os.Chmod(cache, 0o500); err != nil {
		t.Fatalf("Chmod(%s): %v", cache, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(cache, 0o700); err != nil {
			t.Errorf("Chmod(%s): %v", cache, err)
		}
	})

	_, stderr, code := runRename(t, dir, &markedBox{marker: namedA}, "a", "b")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for an inventory smith could not write")
	}
	for _, want := range []string{
		`the box's marker records the name "b", but the inventory still registers it as "a"`,
		"Reconcile them by running the rename again once the inventory can be written:\n  smith machine rename a b\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to say %q", stderr, want)
		}
	}
}

func TestMachineHelpListsRename(t *testing.T) {
	stdout, _, code := runMachine(t, t.TempDir(), &refusingSSH{}, "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "\n  rename ") {
		t.Errorf("machine --help = %q, want it to list rename", stdout)
	}
}

func TestMachineRenameHelpSaysTheProviderMarkerKeepsTheOldName(t *testing.T) {
	stdout, _, code := runMachine(t, t.TempDir(), &refusingSSH{}, "rename", "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "provider-side marker is not renamed") {
		t.Errorf("machine rename --help = %q, want it to say the provider-side marker is not renamed", stdout)
	}
}

func TestMachineRenameRefusesAnInventoryANewerSmithOwnsBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	const before = `{"schema_version":99,"boxes":{"a":{"target":"smith@100.92.14.7"}}}`
	writeInventory(t, dir, before)
	box := &markedBox{marker: namedA}

	_, stderr, code := runRename(t, dir, box, "a", "b")

	if code != 1 {
		t.Errorf("exit code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if box.marker != namedA {
		t.Errorf("marker = %q, want it unchanged at %q", box.marker, namedA)
	}
}

func TestMachineRenameRefusesAMalformedMarkerWithoutChangingAnything(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	const malformed = `{"name": "a", "name": "a"}`
	box := &markedBox{marker: malformed}

	_, stderr, code := runRename(t, dir, box, "a", "b")

	if code != 1 || !strings.HasPrefix(stderr, "rename refused: ") {
		t.Errorf("exit code = %d, stderr = %q, want 1 and a rename refusal", code, stderr)
	}
	if box.marker != malformed {
		t.Errorf("marker = %q, want it unchanged at %q", box.marker, malformed)
	}
	if got := inventoryContent(t, dir); got != registeredA {
		t.Errorf("inventory = %q, want it unchanged at %q", got, registeredA)
	}
}
