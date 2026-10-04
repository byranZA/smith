package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// tailscaleKeyRef is the auth key reference that keeps a tailscale run off the terminal prompt.
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

// accessPrecedence is the spec's access precedence table; an empty declaration declares nothing.
var accessPrecedence = []struct {
	preference, blueprint, flag string
	resolved, origin            string
}{
	{"", "", "", "public", "built-in default"},
	{"tailscale", "", "", "tailscale", "preferences"},
	{"", "tailscale", "", "tailscale", "blueprint"},
	{"public", "tailscale", "", "tailscale", "blueprint"},
	{"tailscale", "public", "", "public", "blueprint"},
	{"", "tailscale", "public", "public", "flag"},
	{"public", "public", "tailscale", "tailscale", "flag"},
}

// runPrecedenceRow sets up a fresh box under one precedence row, returning the fake box and stdout.
func runPrecedenceRow(t *testing.T, preference, blueprintAccess, flag string) (*setupSSH, string) {
	t.Helper()
	dir := t.TempDir()
	if preference != "" {
		writePreferences(t, dir, "access: "+preference+"\n")
	}
	body := ""
	if blueprintAccess != "" {
		body = "access: " + blueprintAccess + "\n"
	}
	writeBlueprint(t, dir, "acme", body)
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "--blueprint", "acme")
	if flag != "" {
		args = append(args, "--access", flag)
	}
	stdout, stderr, code := runSetup(t, dir, ssh, append(args, "root@203.0.113.10")...)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	return ssh, stdout
}

func TestSetupProvisionsTheBoxWithTheMostSpecificAccess(t *testing.T) {
	for _, tc := range accessPrecedence {
		t.Run(tc.preference+"/"+tc.blueprint+"/"+tc.flag, func(t *testing.T) {
			ssh, _ := runPrecedenceRow(t, tc.preference, tc.blueprint, tc.flag)

			if got := ssh.setupFlag("--access"); got != tc.resolved {
				t.Errorf("box provisioned with --access %q, want %q", got, tc.resolved)
			}
		})
	}
}

func TestSetupReportsTheAccessItChoseAndWhereItCameFrom(t *testing.T) {
	for _, tc := range accessPrecedence {
		t.Run(tc.preference+"/"+tc.blueprint+"/"+tc.flag, func(t *testing.T) {
			_, stdout := runPrecedenceRow(t, tc.preference, tc.blueprint, tc.flag)

			want := "access: " + tc.resolved + " (" + tc.origin + ")\n"
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout = %q, want the line %q", stdout, want)
			}
		})
	}
}

func TestSetupOpensPublicSSHOnAFreshBoxWhileItIsReachedPublicly(t *testing.T) {
	for _, access := range []string{"public", "tailscale"} {
		t.Run(access, func(t *testing.T) {
			ssh, _ := runPrecedenceRow(t, "", "", access)

			if got := ssh.setupFlag("--public-ssh"); got != "open" {
				t.Errorf("box provisioned with --public-ssh %q, want %q", got, "open")
			}
		})
	}
}

func TestSetupWithNoConfigHomeProvisionsPublicAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	ssh := &setupSSH{}

	stdout, stderr, code := runSetup(t, dir, ssh, "--name", "dev", "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := ssh.setupFlag("--access"); got != "public" {
		t.Errorf("box provisioned with --access %q, want %q", got, "public")
	}
	if !strings.Contains(stdout, "access: public (built-in default)\n") {
		t.Errorf("stdout = %q, want the line %q", stdout, "access: public (built-in default)")
	}
}

func TestSetupReportsTheAccessBeforeThePreflightReport(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "--blueprint", "acme", "root@203.0.113.10")
	stdout, _, _ := runSetup(t, dir, ssh, args...)

	access := strings.Index(stdout, "access: tailscale (blueprint)\n")
	preflight := strings.Index(stdout, "preflight passed\n")
	if access < 0 || preflight < 0 || access > preflight {
		t.Errorf("stdout = %q, want the access line before the preflight report", stdout)
	}
}

func TestSetupRefusesAResolvedTailscaleAccessFromAnOffTailnetMachine(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	ssh := &setupSSH{adminOffTailnet: true}

	args := append(tailscaleKeyRef(t), "--blueprint", "acme", "root@203.0.113.10")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 2 {
		t.Errorf("exit code = %d, want 2, a gate rejection (stderr: %s)", code, stderr)
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want the box never reached", ssh.targets)
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

func TestSetupNamesTheMalformedPreferencesFileItRefused(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: [unclosed\n")

	_, stderr, _ := runSetup(t, dir, &setupSSH{}, "root@203.0.113.10")

	if !strings.Contains(stderr, filepath.Join(dir, "preferences.yaml")) {
		t.Errorf("stderr = %q, want it to name the preferences file", stderr)
	}
}

func TestSetupRefusesAnUnknownBlueprintBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{}

	_, stderr, code := runSetup(t, dir, ssh, "--blueprint", "missing", "root@203.0.113.10")

	if code == 0 {
		t.Errorf("exit code = 0, want setup refused (stderr: %s)", stderr)
	}
	if !strings.Contains(stderr, "missing") {
		t.Errorf("stderr = %q, want it to name the blueprint %q", stderr, "missing")
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want the box never reached", ssh.targets)
	}
}

// markerOnTailscale is the marker a box smith already set up with tailscale access carries.
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
	if got := ssh.setupFlag("--access"); got != "public" {
		t.Errorf("box provisioned with --access %q, want %q", got, "public")
	}
	if got := ssh.setupFlag("--public-ssh"); got != "open" {
		t.Errorf("box provisioned with --public-ssh %q, want %q", got, "open")
	}
}

func TestSetupKeepsPublicSSHClosedWhenTheTailscaleBoxIsRerunAsTailscale(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\n")
	ssh := &setupSSH{marker: markerOnTailscale, tailnetIP: "100.92.14.7"}

	args := append(tailscaleKeyRef(t), "smith@100.92.14.7")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := ssh.setupFlag("--public-ssh"); got != "closed" {
		t.Errorf("box provisioned with --public-ssh %q, want %q", got, "closed")
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
