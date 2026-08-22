package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// runCheck runs `blueprint check` against a config home rooted at dir and
// returns what landed on each stream plus the process exit code.
func runCheck(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newBlueprintCmd(func() (config.Home, error) { return config.NewHome(dir), nil })
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestBlueprintCheckReportsAValidBlueprint(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\nrepos:\n  - url: git@github.com:acme/api.git\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "acme") || !strings.Contains(stdout, "valid") {
		t.Errorf("stdout = %q, want it to report the blueprint acme as valid", stdout)
	}
}

func TestBlueprintCheckRefusesAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\nterminals: tmux\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a blueprint declaring an unknown field")
	}
	if stderr == "" {
		t.Error("stderr = empty, want the refusal reported there")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed for an invalid blueprint", stdout)
	}
}

func TestBlueprintCheckReportsAMissingBlueprint(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := runCheck(t, dir, "check", "staging")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a blueprint that does not exist")
	}
	if !strings.Contains(stderr, "staging") {
		t.Errorf("stderr = %q, want it to name the blueprint it looked for", stderr)
	}
}

func TestBlueprintCheckCreatesNothing(t *testing.T) {
	dir := t.TempDir()

	if _, _, code := runCheck(t, dir, "check", "acme"); code == 0 {
		t.Fatal("exit code = 0, want non-zero against an empty config home")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read config home: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("config home holds %d entries after check, want it untouched", len(entries))
	}
}

func writeBlueprint(t *testing.T, dir, name, body string) {
	t.Helper()
	blueprints := filepath.Join(dir, "blueprints")
	if err := os.MkdirAll(blueprints, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", blueprints, err)
	}
	if err := os.WriteFile(filepath.Join(blueprints, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write blueprint %s: %v", name, err)
	}
}

func TestBlueprintCheckReportsEveryErrorOnStderr(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\nterminals: tmux\nsessions: 3\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for an invalid blueprint", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want no resolved configuration printed", stdout)
	}
	for _, want := range []string{"line 2", `unknown field "terminals"`, "line 3", `unknown field "sessions"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
	for _, leak := range []string{"blueprint.Blueprint", "main.", "not found in type"} {
		if strings.Contains(stderr, leak) {
			t.Errorf("stderr = %q, want no %q in an operator-facing report", stderr, leak)
		}
	}
}

func TestBlueprintCheckReportsAMalformedBlueprint(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: [unclosed\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for a malformed blueprint", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed for a malformed blueprint", stdout)
	}
	if !strings.Contains(stderr, "malformed") || !strings.Contains(stderr, "line 1") {
		t.Errorf("stderr = %q, want it to report the file as malformed with a position", stderr)
	}
}

func TestBlueprintCheckWithNoBlueprintReportsAnAbsentPreferencesFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent-home")

	stdout, stderr, code := runCheck(t, dir, "check")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for an absent config home (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "no preferences file") {
		t.Errorf("stdout = %q, want it to report that there is no preferences file", stdout)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("config home exists after check, want it not created")
	}
}

func TestBlueprintCheckReadsABlueprintPathVerbatim(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "shared", "terminals: tmux\n") // invalid, and must not be read
	outside := filepath.Join(t.TempDir(), "shared.yaml")
	if err := os.WriteFile(outside, []byte("access: public\n"), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}

	stdout, stderr, code := runCheck(t, dir, "check", outside)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint outside the config home (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, outside) {
		t.Errorf("stdout = %q, want it to name the file %q that was read", stdout, outside)
	}
}

func TestBlueprintCheckRefusesANameCarryingAnExtension(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme.yaml")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for a name carrying an extension", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed for a refused blueprint name", stdout)
	}
	if !strings.Contains(stderr, "extension") || !strings.Contains(stderr, `"acme"`) {
		t.Errorf("stderr = %q, want it to refuse the extension and name %q instead", stderr, "acme")
	}
}

func writePreferences(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write preferences: %v", err)
	}
}

func TestBlueprintCheckWithNoBlueprintValidatesThePreferences(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\nterminal: tmux\n")

	stdout, stderr, code := runCheck(t, dir, "check")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for valid preferences (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "valid") || !strings.Contains(stdout, "preferences.yaml") {
		t.Errorf("stdout = %q, want it to report the preferences file as valid", stdout)
	}
}

func TestBlueprintCheckWithNoBlueprintRefusesACollectionInPreferences(t *testing.T) {
	for _, field := range []string{"repos", "placements", "packages", "tools", "env"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			writePreferences(t, dir, field+":\n")

			stdout, stderr, code := runCheck(t, dir, "check")

			if code != 1 {
				t.Fatalf("exit code = %d, want 1 for %q in preferences (stdout: %s)", code, field, stdout)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing printed for invalid preferences", stdout)
			}
			if !strings.Contains(stderr, field) {
				t.Errorf("stderr = %q, want it to name %q as the refused field", stderr, field)
			}
		})
	}
}

func TestBlueprintCheckFailsWhenThePreferencesAreInvalid(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "repos:\n  - url: git@github.com:acme/api.git\n")
	writeBlueprint(t, dir, "acme", "access: tailscale\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for a valid blueprint under invalid preferences", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed while the preferences are invalid", stdout)
	}
	if !strings.Contains(stderr, "preferences") || !strings.Contains(stderr, "repos") {
		t.Errorf("stderr = %q, want it to report the preferences error", stderr)
	}
}

func TestBlueprintCheckPrintsTheResolvedConfigurationWithEachOrigin(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "workspace: ~/dev\n")
	writeBlueprint(t, dir, "acme", "access: tailscale\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"access", "tailscale", "blueprint", "workspace", "~/dev", "preferences", "terminal", "tmux", "built-in default"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want the resolved configuration to contain %q", stdout, want)
		}
	}
}

func TestBlueprintCheckTakesTheAccessFlagOverEverything(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: public\n")
	writeBlueprint(t, dir, "acme", "access: public\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme", "--access", "tailscale")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "tailscale (flag)") {
		t.Errorf("stdout = %q, want access resolved to tailscale from the flag", stdout)
	}
}

func TestBlueprintCheckRefusesAnUnrecognisedAccessFlag(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: public\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme", "--access", "wireguard")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for an access mode smith does not recognise")
	}
	if !strings.Contains(stderr, "wireguard") {
		t.Errorf("stderr = %q, want it to name the value it refused", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want no resolved configuration printed", stdout)
	}
}

func TestBlueprintCheckResolvesPreferencesWithNoBlueprintNamed(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\n")

	stdout, stderr, code := runCheck(t, dir, "check")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for valid preferences (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "tailscale (preferences)") {
		t.Errorf("stdout = %q, want access resolved to tailscale from preferences", stdout)
	}
}

// hetznerPreferences is a whole adapter written where a one-account operator
// writes one: beside their preferences, complete.
const hetznerPreferences = `provider:
  create: [hcloud, server, create, --name, "{{name}}"]
  list: [hcloud, server, list, -o, json]
  destroy: [hcloud, server, delete, "{{id}}"]
  requires: [HCLOUD_TOKEN]
  ssh_key: byran@laptop
  marker:
    arg: smith
    read: .labels.smith
  extract:
    id: .id
    ip: .public_net.ipv4.ip
`

func TestBlueprintCheckReportsABlueprintProviderReplacingThePreference(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, hetznerPreferences)
	writeBlueprint(t, dir, "acme", "provider:\n  create: [doctl, compute, droplet, create]\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "doctl (blueprint, replacing the preference)") {
		t.Errorf("stdout = %q, want the adapter reported as the blueprint's, replacing the preference", stdout)
	}
	if strings.Contains(stdout, "hcloud") {
		t.Errorf("stdout = %q, want no trace of the replaced Hetzner adapter", stdout)
	}
}

func TestBlueprintCheckReportsAnInheritedPreferenceProvider(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, hetznerPreferences)
	writeBlueprint(t, dir, "acme", "access: tailscale\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a valid blueprint (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "hcloud (preferences)") {
		t.Errorf("stdout = %q, want the preference adapter inherited entire", stdout)
	}
}
