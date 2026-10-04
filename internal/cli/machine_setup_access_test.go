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

// markerOnTailscale is the marker a box smith already set up with tailscale
// access carries: public SSH closed, reached over the tailnet.
const markerOnTailscale = `{"schema_version":2,"access_mode":"tailscale","name":"devbox"}`

func TestSetupRefusesToReopenPublicSSHOnATailscaleBox(t *testing.T) {
	cases := []struct {
		name   string
		config func(t *testing.T, dir string)
		args   []string
		origin string
	}{
		{
			name:   "built-in default",
			config: func(*testing.T, string) {},
			origin: "built-in default",
		},
		{
			name:   "preferences",
			config: func(t *testing.T, dir string) { writePreferences(t, dir, "access: public\n") },
			origin: "preferences",
		},
		{
			name:   "blueprint",
			config: func(t *testing.T, dir string) { writeBlueprint(t, dir, "acme", "access: public\n") },
			args:   []string{"--blueprint", "acme"},
			origin: "blueprint",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.config(t, dir)
			ssh := &setupSSH{marker: markerOnTailscale}

			args := append(tc.args, "smith@100.92.14.7")
			_, stderr, code := runSetup(t, dir, ssh, args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2, a gate rejection (stderr: %s)", code, stderr)
			}
			want := "setup refused: devbox was provisioned with access tailscale, but this run resolves access: public (" + tc.origin + "); " +
				"pass --access tailscale to keep it, or --access public to reopen public SSH\n"
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
			if ssh.hardened || ssh.closed {
				t.Errorf("the box ran a mutating phase (commands %v), want it untouched", ssh.commands)
			}
		})
	}
}

func TestSetupReopensPublicSSHOnATailscaleBoxWhenTheFlagSaysSo(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{marker: markerOnTailscale}

	_, stderr, code := runSetup(t, dir, ssh, "--access", "public", "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !ssh.hardened {
		t.Error("the base layer did not run, want the box provisioned with public access")
	}
}

func TestSetupMovesAPublicBoxOntoTheTailnetWithoutTheFlag(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\n")
	ssh := &setupSSH{marker: markerNamedDev, tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "root@203.0.113.10")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want the box registered at its tailnet address", got)
	}
}

func TestSetupGuardsNoAccessOnABoxThatRecordsNone(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{marker: `{"schema_version":2,"name":"devbox"}`}

	_, stderr, code := runSetup(t, dir, ssh, "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !ssh.hardened {
		t.Error("the base layer did not run, want the box provisioned with public access")
	}
}
