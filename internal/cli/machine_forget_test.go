package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/provider"
)

// runForget runs `machine forget` against a config home rooted at dir. It is
// driven through the real machine command tree, whose only ways out to a box
// are the runner and dialer given here — both of which fail the test if used,
// because forgetting touches no box.
func runForget(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		idleRunner{t: t}, idleDialer{t: t}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"forget"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestMachineForgetRemovesTheEntry(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"},`+
		`"scratch":{"target":"smith@203.0.113.42"}}}`)

	stdout, stderr, code := runForget(t, dir, "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want no box named dev", got)
	}
	if !strings.Contains(got, `"scratch"`) {
		t.Errorf("inventory = %q, want the other box left registered", got)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("stdout = %q, want it to name the box it forgot", stdout)
	}
}

func TestMachineForgetNamesTheTargetThatAddsTheBoxBack(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	stdout, stderr, code := runForget(t, dir, "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith machine add smith@100.92.14.7") {
		t.Errorf("stdout = %q, want it to name the command that adds the box back", stdout)
	}
}

func TestMachineForgetRefusesANameThatIsNotRegistered(t *testing.T) {
	dir := t.TempDir()
	const before = `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`
	writeInventory(t, dir, before)

	stdout, stderr, code := runForget(t, dir, "staging")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a name no box is registered under (stdout: %s)", stdout)
	}
	if out := stdout + stderr; !strings.Contains(out, "staging") {
		t.Errorf("output = %q, want it to report that no box named staging is registered", out)
	}
	if got := inventoryContent(t, dir); got != before {
		t.Errorf("inventory = %q, want it unchanged at %q", got, before)
	}
}

func TestMachineForgetOnAMissingInventoryCreatesNothing(t *testing.T) {
	dir := t.TempDir()

	_, _, code := runForget(t, dir, "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when nothing is registered at all")
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a refused forget to create nothing", err)
	}
}

func TestMachineForgetLetsTheBoxBeAddedBack(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"203.0.113.10":{"target":"smith@203.0.113.10"}}}`)

	if _, stderr, code := runForget(t, dir, "203.0.113.10"); code != 0 {
		t.Fatalf("forget exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	_, stderr, code := runAdd(t, dir, &provisionedSSH{markerJSON: provisioned}, "smith@203.0.113.10")

	if code != 0 {
		t.Fatalf("add exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want the forgotten box registered again", got)
	}
}

func TestMachineForgetRefusesToWriteAnInventoryANewerSmithOwns(t *testing.T) {
	dir := t.TempDir()
	const before = `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`
	writeInventory(t, dir, before)

	stdout, stderr, code := runForget(t, dir, "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for an inventory a newer smith owns")
	}
	if out := stdout + stderr; !strings.Contains(out, "upgrade smith") {
		t.Errorf("output = %q, want it to name upgrading as the fix", out)
	}
	if got := inventoryContent(t, dir); got != before {
		t.Errorf("inventory = %q, want it unchanged at %q", got, before)
	}
}
