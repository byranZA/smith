package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBlueprintCheckJSONOfANamedBlueprintPrintsEachValueWithItsOriginToken(t *testing.T) {
	t.Parallel()
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
		`"workspace":{"value":"~/dev","origin":"preferences"},"packages":[{"value":"jq","origin":"blueprint"}]},"references":{"resolve":true},"problems":[]}` + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestBlueprintCheckJSONMarksAProviderThatReplacesThePreference(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	stdout, stderr, code := runCheck(t, t.TempDir(), "check", "ghost", "--json")

	if code != 1 || stdout != "" || !strings.Contains(stderr, "ghost") {
		t.Errorf("exit code = %d, stdout = %q, stderr = %q, want 1, empty stdout and the name on stderr", code, stdout, stderr)
	}
}

func TestBlueprintCheckJSONPrintsPlacementsAndEachRepoWholeConfiguration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", `placements:
  - from: "file:secrets/env"
    to: ~/.env
    mode: converge
    perms: "0600"
tools:
  go: "1.22"
repos:
  - url: https://github.com/acme/api.git
    base: main
    tools:
      node: "20"
    env:
      MODE: "literal:dev"
    placements:
      - from: "file:local.json"
        to: config/local.json
`)

	stdout, stderr, code := runCheck(t, dir, "check", "acme", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	want := `"repos":[{"name":{"value":"api","origin":"blueprint"},"url":{"value":"https://github.com/acme/api.git","origin":"blueprint"},` +
		`"base":{"value":"main","origin":"blueprint"},` +
		`"tools":{"node":{"value":"20","origin":"blueprint"}},"env":{"MODE":{"value":"literal:dev","origin":"blueprint"}},` +
		`"placements":[{"from":"file:local.json","to":"config/local.json","origin":"blueprint"}]}],` +
		`"tools":{"go":{"value":"1.22","origin":"blueprint"}},` +
		`"placements":[{"from":"file:secrets/env","to":"~/.env","mode":"converge","perms":"0600","origin":"blueprint"}]},"references":{"resolve":false,"unresolved":[{"scope":"~/.env","ref":"file:secrets/env"`
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to contain\n%s", stdout, want)
	}
}

func TestBlueprintCheckJSONReportsUnresolvedReferencesAndEmptyLiteralsApart(t *testing.T) {
	unsetEnv(t, "SMITH_TEST_UNSET_TOKEN")
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "env:\n  GH_TOKEN: env:SMITH_TEST_UNSET_TOKEN\n  REGION: \"literal:\"\n")

	stdout, stderr, code := runCheck(t, dir, "check", "acme", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `"references":{"resolve":false,` +
		`"unresolved":[{"scope":"GH_TOKEN","ref":"env:SMITH_TEST_UNSET_TOKEN","reason":"read what this blueprint value names: environment variable \"SMITH_TEST_UNSET_TOKEN\": secret reference resolved to nothing"}],` +
		`"empty_literals":[{"scope":"REGION","ref":"literal:","reason":`
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to contain\n%s", stdout, want)
	}
}

func TestBlueprintCheckJSONReportsResolvingReferences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "terminal: tmux\n")

	stdout, _, _ := runCheck(t, dir, "check", "acme", "--json")

	want := `"references":{"resolve":true},"problems":[]}`
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to contain\n%s", stdout, want)
	}
}
