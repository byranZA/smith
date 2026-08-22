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
	"github.com/byranZA/smith/internal/inventory"
)

// answeringSSH stands in for the local ssh binary in front of a box that
// answers: every launch gets in, and the destination it was pointed at is
// recorded so a test can see which target smith proved.
type answeringSSH struct {
	targets []string
}

// Run records the ssh destination and lets the command through.
func (s *answeringSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	if name == "ssh" && len(args) >= 2 {
		s.targets = append(s.targets, args[len(args)-2])
	}
	return nil
}

// concludeAt runs the post-phase conclusion against a config home rooted at
// dir, returning what landed on each stream plus the exit code.
func concludeAt(t *testing.T, dir string, exec interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}, c setupConclusion) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	err := concludeSetup(context.Background(), exec, config.NewHome(dir), c, &out, &errBuf)
	return out.String(), errBuf.String(), codeFromError(err)
}

func TestSetupProvesTheSmithUserFromTheOperatorsMachineBeforeRegistering(t *testing.T) {
	dir := t.TempDir()
	ssh := &answeringSSH{}

	stdout, stderr, code := concludeAt(t, dir, ssh, setupConclusion{accessMode: "public", names: inventory.Naming{Host: "203.0.113.10"}})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a box that answered as the smith user (stderr: %s)", code, stderr)
	}
	if len(ssh.targets) == 0 || ssh.targets[0] != "smith@203.0.113.10" {
		t.Errorf("ssh targets = %v, want a connection opened as smith@203.0.113.10", ssh.targets)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"203.0.113.10"`) || !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want the host mapped to smith@203.0.113.10", got)
	}
	if !strings.Contains(stdout, "203.0.113.10") {
		t.Errorf("stdout = %q, want the box reported as registered", stdout)
	}
}

func TestSetupRegistersTheTailnetAddressWithoutASecondProbe(t *testing.T) {
	dir := t.TempDir()
	ssh := &answeringSSH{}

	_, stderr, code := concludeAt(t, dir, ssh, setupConclusion{
		accessMode: "tailscale", tailnetIP: "100.92.14.7",
		names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a tailnet address establish already proved (stderr: %s)", code, stderr)
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want no second probe of an address establish already proved", ssh.targets)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want dev mapped to the tailnet address", got)
	}
	if strings.Contains(got, "203.0.113.10") {
		t.Errorf("inventory = %q, want the public address never stored in tailscale mode", got)
	}
}

func TestSetupNamesTheBoxInTheTailscaleReRunLine(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "tailscale", tailnetIP: "100.92.14.7",
		names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith machine setup dev") {
		t.Errorf("stdout = %q, want the re-run line to name the box", stdout)
	}
	if strings.Contains(stdout, "smith-203.0.113.10") {
		t.Errorf("stdout = %q, want no derived tailnet name in the re-run line", stdout)
	}
}

func TestSetupRegistersUnderTheNameTheOperatorChose(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public", names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want the box registered as dev", got)
	}
}

func TestSetupReportsAProvisionedButUnregisteredBoxWhenItCannotProveReach(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := concludeAt(t, dir, &refusingSSH{}, setupConclusion{
		accessMode: "public", names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1: the phases completed, so a probe failure is partial", code)
	}
	out := stdout + stderr
	for _, want := range []string{"provisioned", "smith@203.0.113.10", "smith machine add"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to name %q", out, want)
		}
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a box smith could not reach left unregistered", err)
	}
}

func TestSetupCreatesTheConfigHomeWhenItRegisters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")

	_, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public", names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(gitignore): %v", err)
	}
	if !strings.Contains(string(data), "cache/") {
		t.Errorf("gitignore = %q, want the cache kept out of version control", data)
	}
}

func TestMachineSetupRegistersNothingWhenThePhasesNeverRan(t *testing.T) {
	dir := t.TempDir()
	ssh := &refusingSSH{}

	_, _, code := runMachine(t, dir, ssh, "setup", "root@203.0.113.10")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a setup that never got in")
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a failed setup to register nothing", err)
	}
}
