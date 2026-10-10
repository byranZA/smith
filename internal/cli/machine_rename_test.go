package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/provider"
)

// markedBox is the ssh binary in front of one box, serving and replacing its marker.
type markedBox struct {
	marker      string
	unreachable bool
}

func (b *markedBox) Run(_ context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if name != "ssh" || len(args) == 0 {
		return nil
	}
	if b.unreachable {
		if _, err := io.WriteString(stderr, "ssh: connect to host 100.92.14.7 port 22: Connection timed out\n"); err != nil {
			return err
		}
		return refusedExit{}
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

func runRename(t *testing.T, dir string, box *markedBox, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		box, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"rename", "--"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// lockInventory makes the inventory under dir unwritable until unlock restores it.
func lockInventory(t *testing.T, dir string) (unlock func()) {
	t.Helper()
	cache := config.NewHome(dir).CachePath()
	if err := os.Chmod(cache, 0o500); err != nil {
		t.Fatalf("Chmod(%s): %v", cache, err)
	}
	unlock = func() {
		if err := os.Chmod(cache, 0o700); err != nil {
			t.Errorf("Chmod(%s): %v", cache, err)
		}
	}
	t.Cleanup(unlock)
	return unlock
}

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

const namedB = `{
  "schema_version": 2,
  "smith_version": "0.9.1",
  "access_mode": "tailscale",
  "name": "b",
  "blueprint": "go-dev",
  "completed_phases": ["packages", "smith-user"],
  "updated_at": "2026-09-01T08:00:00Z"
}
`

const registeredA = `{"schema_version":1,"boxes":{"a":{"target":"smith@100.92.14.7"},` +
	`"other":{"target":"smith@100.92.14.8"}}}`

// listedB is machine list after box "a" of registeredA is renamed "b".
const listedB = "NAME   TARGET\n" +
	"b      smith@100.92.14.7\n" +
	"other  smith@100.92.14.8\n" +
	"\n2 boxes\n"

func TestMachineRenameMovesTheEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)

	if _, stderr, code := runRename(t, dir, &markedBox{marker: namedA}, "a", "b"); code != 0 {
		t.Fatalf("rename a b exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	if listed, _, _ := runList(t, dir); listed != listedB {
		t.Errorf("machine list after rename a b = %q, want %q", listed, listedB)
	}
}

func TestMachineRenameRewritesOnlyTheMarkersName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	box := &markedBox{marker: namedA}

	if _, stderr, code := runRename(t, dir, box, "a", "b"); code != 0 {
		t.Fatalf("rename a b exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	if box.marker != namedB {
		t.Errorf("marker after rename a b = %q, want %q", box.marker, namedB)
	}
}

func TestMachineRenameReportsTheRenameAndHowToReachTheBox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)

	stdout, stderr, code := runRename(t, dir, &markedBox{marker: namedA}, "a", "b")

	if code != 0 {
		t.Fatalf("rename a b exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	const want = `renamed "a" to "b"
registered smith@100.92.14.7 as "b"

Reach it by name from now on:
  smith machine status b
  smith machine setup b
`
	if stdout != want {
		t.Errorf("rename a b stdout = %q, want %q", stdout, want)
	}
}

func TestMachineRenameHintsTheBlueprintToABoxBuiltFromNone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	const unbuilt = `{"schema_version": 2, "access_mode": "public", "name": "a", "completed_phases": []}`

	stdout, stderr, code := runRename(t, dir, &markedBox{marker: unbuilt}, "a", "b")

	const want = "\n  smith machine setup b --blueprint <blueprint>\n"
	if code != 0 || !strings.HasSuffix(stdout, want) {
		t.Errorf("rename a b = exit %d, stdout %q; want exit 0 ending %q (stderr: %s)", code, stdout, want, stderr)
	}
}

// renameRefusals are renames refused before anything changes.
var renameRefusals = []struct {
	name        string
	inventory   string
	marker      string
	unreachable bool
	from, to    string
	code        int
	want        string
}{
	{"an unknown box", registeredA, namedA, false, "staging", "b", 1, `no box named "staging" is registered`},
	{"a literal target", registeredA, namedA, false, "smith@100.92.14.7", "b", 1, `no box named "smith@100.92.14.7" is registered`},
	{"the current name", registeredA, namedA, false, "a", "a", 1, `already named "a"`},
	{"an empty name", registeredA, namedA, false, "a", "", 1, "no box name"},
	{"a name another box holds", registeredA, namedA, false, "a", "other", 1, `"other" already names a different box`},
	{
		"an inventory a newer smith wrote", `{"schema_version":99,"boxes":{"a":{"target":"smith@100.92.14.7"}}}`,
		namedA, false, "a", "b", 1, "upgrade smith",
	},
	{
		"an unreachable box", registeredA, namedA, true, "a", "b", 3,
		"rename refused: ssh smith@100.92.14.7: could not connect to box: the box did not answer; " +
			"check the address, that sshd is listening on port 22, and the provider's firewall\n",
	},
	{"a box carrying no marker", registeredA, "", false, "a", "b", 2, "smith machine setup"},
	{"a malformed marker", registeredA, `{"name": "a", "name": "a"}`, false, "a", "b", 1, "malformed marker: it records more than one name"},
}

func TestMachineRenameRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range renameRefusals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeInventory(t, dir, tt.inventory)

			_, stderr, code := runRename(t, dir, &markedBox{marker: tt.marker, unreachable: tt.unreachable}, tt.from, tt.to)

			if code != tt.code || !strings.Contains(stderr, tt.want) {
				t.Errorf("rename %q %q = exit %d, stderr %q; want exit %d saying %q", tt.from, tt.to, code, stderr, tt.code, tt.want)
			}
		})
	}
}

func TestMachineRenameRefusalChangesNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range renameRefusals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeInventory(t, dir, tt.inventory)
			before, _, _ := runList(t, dir)
			box := &markedBox{marker: tt.marker, unreachable: tt.unreachable}

			runRename(t, dir, box, tt.from, tt.to)

			if after, _, _ := runList(t, dir); after != before || box.marker != tt.marker {
				t.Errorf("rename %q %q left machine list %q and marker %q, want %q and %q",
					tt.from, tt.to, after, box.marker, before, tt.marker)
			}
		})
	}
}

func TestMachineRenameNamesTheDisagreementWhenTheInventoryWriteFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	lockInventory(t, dir)

	_, stderr, code := runRename(t, dir, &markedBox{marker: namedA}, "a", "b")

	const want = `the box's marker records the name "b", but the inventory still registers it as "a"`
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("rename a b = exit %d, stderr %q; want exit 1 saying %q", code, stderr, want)
	}
}

func TestMachineRenameGivesAReconcileCommandThatKeepsEachNameOneArgument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		from, to string
		want     string
	}{
		{"plain names", "a", "b", "smith machine rename -- a b"},
		{"spaces", "old box", "new box", "smith machine rename -- 'old box' 'new box'"},
		{"a quote", "it's", "b", `smith machine rename -- 'it'\''s' b`},
		{"shell metacharacters", "a", "$(reboot);b", "smith machine rename -- a '$(reboot);b'"},
		{"leading dashes", "-a", "--b", "smith machine rename -- -a --b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeInventory(t, dir, fmt.Sprintf(`{"schema_version":1,"boxes":{%q:{"target":"smith@100.92.14.7"}}}`, tt.from))
			lockInventory(t, dir)

			_, stderr, _ := runRename(t, dir, &markedBox{marker: namedA}, tt.from, tt.to)

			if want := "\n  " + tt.want + "\n"; !strings.Contains(stderr, want) {
				t.Errorf("rename %q %q stderr = %q, want it to give %q", tt.from, tt.to, stderr, tt.want)
			}
		})
	}
}

func TestMachineRenameRunAgainReconcilesAPartialRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, registeredA)
	box := &markedBox{marker: namedA}
	unlock := lockInventory(t, dir)
	if _, stderr, code := runRename(t, dir, box, "a", "b"); code == 0 {
		t.Fatalf("first rename a b exit code = 0, want a partial rename (stderr: %s)", stderr)
	}
	unlock()

	if _, stderr, code := runRename(t, dir, box, "a", "b"); code != 0 {
		t.Fatalf("rename a b again exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	if listed, _, _ := runList(t, dir); listed != listedB {
		t.Errorf("machine list after rename a b again = %q, want %q", listed, listedB)
	}
}

func TestMachineHelpListsRename(t *testing.T) {
	t.Parallel()
	stdout, _, code := runMachine(t, t.TempDir(), &refusingSSH{}, "--help")

	if code != 0 || !strings.Contains(stdout, "\n  rename ") {
		t.Errorf("machine --help = exit %d, %q; want exit 0 listing rename", code, stdout)
	}
}

func TestMachineRenameHelpSaysTheProviderMarkerKeepsTheOldName(t *testing.T) {
	t.Parallel()
	stdout, _, code := runMachine(t, t.TempDir(), &refusingSSH{}, "rename", "--help")

	const want = "provider-side marker is not renamed"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("machine rename --help = exit %d, %q; want exit 0 saying %q", code, stdout, want)
	}
}
