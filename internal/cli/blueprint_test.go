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
