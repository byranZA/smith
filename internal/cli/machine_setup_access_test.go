package cli

import (
	"strings"
	"testing"
)

// tailscaleKeyRef is the auth key reference a run that may resolve to
// tailscale is driven with, so it never reaches for a terminal prompt whether
// or not the access mode came from its flag.
func tailscaleKeyRef(t *testing.T) []string {
	t.Helper()
	t.Setenv("SMITH_TEST_TAILSCALE_KEY", "tskey-auth-test")
	return []string{"--tailscale-auth-key", "env:SMITH_TEST_TAILSCALE_KEY"}
}

func TestSetupReachesTheBoxOverTheTailnetWhenItsBlueprintAsksForTailscale(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "--blueprint", "acme", "root@203.0.113.10")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want the box registered at its tailnet address", got)
	}
}

func TestSetupReachesTheBoxOverTheTailnetWhenThePreferencesAskForTailscale(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\n")
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "--name", "dev", "root@203.0.113.10")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want the box registered at its tailnet address", got)
	}
}

func TestSetupAccessFlagOutranksTheBlueprintAndThePreferences(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	writePreferences(t, dir, "access: tailscale\n")
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	_, stderr, code := runSetup(t, dir, ssh, "--access", "public", "--blueprint", "acme", "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want the box registered at its public address", got)
	}
}

func TestSetupSaysWhichAccessItChoseAndWhere(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "--blueprint", "acme", "root@203.0.113.10")
	stdout, _, _ := runSetup(t, dir, ssh, args...)

	if !strings.Contains(stdout, "access: tailscale (blueprint)\n") {
		t.Errorf("stdout = %q, want the line %q", stdout, "access: tailscale (blueprint)")
	}
}

func TestSetupRefusesMalformedPreferencesBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: [unclosed\n")
	ssh := &setupSSH{}

	_, stderr, code := runSetup(t, dir, ssh, "root@203.0.113.10")

	if code != 2 {
		t.Errorf("exit code = %d, want 2, a gate rejection (stderr: %s)", code, stderr)
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want the box never reached", ssh.targets)
	}
}
