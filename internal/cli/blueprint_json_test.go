package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBlueprintCheckJSONOfANamedBlueprintPrintsEachValueWithItsOriginToken(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "workspace: ~/dev\n")
	writeBlueprint(t, dir, "acme", "terminal: tmux\npackages: [jq]\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme", "--access", "tailscale", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"preferences":{"path":"` + filepath.Join(dir, "preferences.yaml") + `","found":true},` +
		`"blueprint":{"name":"acme","path":"` + filepath.Join(dir, "blueprints", "acme.yaml") + `"},` +
		`"resolved":{"access":{"value":"tailscale","origin":"flag"},"terminal":{"value":"tmux","origin":"blueprint"},` +
		`"workspace":{"value":"~/dev","origin":"preferences"},"packages":["jq"]},"problems":[]}` + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestBlueprintCheckJSONMarksAProviderThatReplacesThePreference(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, hetznerPreferences)
	writeBlueprint(t, dir, "acme", "provider:\n  create: [doctl, compute, droplet, create]\n")

	stdout, _, code := runCheck(t, dir, "check", "acme", "--json")

	want := `"provider":{"value":"doctl","origin":"blueprint","replaces_preference":true}`
	if code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit code = %d, stdout = %s, want 0 and %s", code, stdout, want)
	}
}

func TestBlueprintCheckJSONWithNoBlueprintReportsThePreferencesAlone(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := runCheck(t, dir, "check", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"preferences":{"path":"` + filepath.Join(dir, "preferences.yaml") + `","found":false},` +
		`"resolved":{"access":{"value":"public","origin":"default"},"terminal":{"value":"tmux","origin":"default"},` +
		`"workspace":{"value":"~/workspace","origin":"default"}},"problems":[]}` + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestBlueprintCheckJSONPrintsProblemsAndExitsOne(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\nterminals: tmux\n")

	stdout, _, code := runCheck(t, dir, "check", "acme", "--json")

	want := `{"schema_version":1,"preferences":{"path":"` + filepath.Join(dir, "preferences.yaml") + `","found":false},` +
		`"blueprint":{"name":"acme","path":"` + filepath.Join(dir, "blueprints", "acme.yaml") + `"},` +
		`"problems":[{"line":2,"message":"unknown field \"terminals\""}]}` + "\n"
	if code != 1 || stdout != want {
		t.Errorf("exit code = %d, stdout =\n%s\nwant 1 and\n%s", code, stdout, want)
	}
}

func TestBlueprintCheckJSONOfAMissingBlueprintPrintsNoDocument(t *testing.T) {
	stdout, stderr, code := runCheck(t, t.TempDir(), "check", "ghost", "--json")

	if code != 1 || stdout != "" || !strings.Contains(stderr, "ghost") {
		t.Errorf("exit code = %d, stdout = %q, stderr = %q, want 1, empty stdout and the name on stderr", code, stdout, stderr)
	}
}
