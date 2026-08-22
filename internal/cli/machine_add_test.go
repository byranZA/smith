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

// provisionedSSH stands in for the local ssh binary in front of a box smith
// already provisioned: every command gets in, and the marker read answers with
// whatever the box carries.
type provisionedSSH struct {
	markerJSON string
	refuse     bool
	cmds       []string
}

// Run answers the remote command ssh was asked to run on the box.
func (s *provisionedSSH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name != "ssh" || len(args) == 0 {
		return nil
	}
	cmd := args[len(args)-1]
	s.cmds = append(s.cmds, cmd)
	if s.refuse {
		return refusedExit{}
	}
	if strings.Contains(cmd, "bootstrap.json") {
		if _, err := io.WriteString(stdout, s.markerJSON); err != nil {
			return err
		}
	}
	return nil
}

// wrote reports whether any command run on the box would have changed it.
func (s *provisionedSSH) wrote() bool {
	for _, cmd := range s.cmds {
		if strings.Contains(cmd, "tee") || strings.Contains(cmd, "mkdir") || strings.Contains(cmd, "bash ") {
			return true
		}
	}
	return false
}

// runAdd runs `machine add` against a config home rooted at dir, reaching the
// box through the given fake ssh.
func runAdd(t *testing.T, dir string, ssh *provisionedSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"add"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// inventoryContent returns the inventory file inside the config home at dir.
func inventoryContent(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(config.NewHome(dir).InventoryPath())
	if err != nil {
		t.Fatalf("ReadFile(inventory): %v", err)
	}
	return string(data)
}

// provisioned is a marker as a box provisioned by this build carries it.
const provisioned = `{"schema_version":1,"smith_version":"dev","access_mode":"public","completed_phases":["packages"]}`

func TestMachineAddRegistersAProvisionedBoxUnderItsHost(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{markerJSON: provisioned}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"203.0.113.10"`) || !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want 203.0.113.10 mapped to smith@203.0.113.10", got)
	}
	if !strings.Contains(stdout, "203.0.113.10") {
		t.Errorf("stdout = %q, want the name the box was registered under", stdout)
	}
	if !strings.Contains(stdout, "no name") {
		t.Errorf("stdout = %q, want it to say the marker recorded no name", stdout)
	}
}

func TestMachineAddCreatesTheConfigHomeAndGitignoresTheCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")
	ssh := &provisionedSSH{markerJSON: provisioned}

	_, stderr, code := runAdd(t, dir, ssh, "smith@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(gitignore): %v", err)
	}
	if !strings.Contains(string(data), "cache/") {
		t.Errorf("gitignore = %q, want it to keep the cache out of version control", data)
	}
}

func TestMachineAddRegistersUnderAnExplicitName(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{markerJSON: provisioned}

	_, stderr, code := runAdd(t, dir, ssh, "smith@100.92.14.7", "--name", "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want the box registered as dev", got)
	}
}

func TestMachineAddRefusesABoxCarryingNoMarker(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@198.51.100.7")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a box smith never set up (stdout: %s)", stdout)
	}
	out := stdout + stderr
	if !strings.Contains(out, "smith machine setup") {
		t.Errorf("output = %q, want it to name the setup command to run first", out)
	}
	if ssh.wrote() {
		t.Errorf("commands run = %v, want the box left unmodified", ssh.cmds)
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a refused add to register nothing", err)
	}
}

func TestMachineAddRefusesAnUnreachableBox(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{refuse: true}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@198.51.100.7")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a box smith could not reach")
	}
	if out := stdout + stderr; !strings.Contains(out, "could not connect") {
		t.Errorf("output = %q, want it to report that smith could not connect", out)
	}
}

func TestMachineAddIsIdempotentForABoxAlreadyRegisteredAtTheSameTarget(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"203.0.113.10":{"target":"smith@203.0.113.10"}}}`)
	ssh := &provisionedSSH{markerJSON: provisioned}

	_, stderr, code := runAdd(t, dir, ssh, "smith@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for re-adding a box at the same target (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); strings.Count(got, `"target"`) != 1 {
		t.Errorf("inventory = %q, want exactly the one entry", got)
	}
}

func TestMachineAddRefusesANameHeldByADifferentBox(t *testing.T) {
	dir := t.TempDir()
	const before = `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`
	writeInventory(t, dir, before)
	ssh := &provisionedSSH{markerJSON: provisioned}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@198.51.100.7", "--name", "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a name that already reaches another box")
	}
	if out := stdout + stderr; !strings.Contains(out, "dev") {
		t.Errorf("output = %q, want it to name the box the name is held by", out)
	}
	if got := inventoryContent(t, dir); got != before {
		t.Errorf("inventory = %q, want it unchanged at %q", got, before)
	}
}

func TestMachineAddRefusesToWriteAnInventoryANewerSmithOwns(t *testing.T) {
	dir := t.TempDir()
	const before = `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`
	writeInventory(t, dir, before)
	ssh := &provisionedSSH{markerJSON: provisioned}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@203.0.113.10")

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
