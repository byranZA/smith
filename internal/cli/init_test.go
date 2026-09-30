package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// runInit runs `smith init` against a config home rooted at dir and returns
// what landed on each stream plus the process exit code.
func runInit(t *testing.T, dir string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newInitCmd(func() (config.Home, error) { return config.NewHome(dir), nil })
	cmd.SetArgs(nil)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestInitReportsEachStarterAsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".smith")

	stdout, stderr, code := runInit(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, path := range []string{filepath.Join(dir, "preferences.yaml"), filepath.Join(dir, "blueprints", "personal.yaml")} {
		if !strings.Contains(stdout, "created "+path) {
			t.Errorf("stdout = %q, want %s reported as created", stdout, path)
		}
	}
}

func TestInitReportsAnExistingFileAsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	prefs := filepath.Join(dir, "preferences.yaml")
	writeFile(t, prefs, "access: tailscale\n")

	stdout, _, _ := runInit(t, dir)

	if !strings.Contains(stdout, "left alone "+prefs) {
		t.Errorf("stdout = %q, want %s reported as left alone", stdout, prefs)
	}
}

func TestInitEndsWithTheNextSteps(t *testing.T) {
	dir := t.TempDir()
	runInit(t, dir)

	// The second run creates nothing and still owes the operator the way on.
	stdout, _, code := runInit(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 on a second run", code)
	}
	want := "smith blueprint check personal\n" +
		"  smith machine setup <login>@<host> --blueprint personal\n"
	if !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout = %q, want it to end with %q", stdout, want)
	}
}

func TestInitFailsNamingThePathItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	blueprints := filepath.Join(dir, "blueprints")
	writeFile(t, blueprints, "not a directory\n")

	_, stderr, code := runInit(t, dir)

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when a starter cannot be written")
	}
	if !strings.Contains(stderr, blueprints) {
		t.Errorf("stderr = %q, want it to name %s", stderr, blueprints)
	}
}

func TestTheStartersPassBlueprintCheck(t *testing.T) {
	dir := t.TempDir()
	runInit(t, dir)

	for _, args := range [][]string{{"check"}, {"check", "personal"}} {
		if _, stderr, code := runCheck(t, dir, args...); code != 0 {
			t.Errorf("blueprint %v exit code = %d, want 0 (stderr: %s)", args, code, stderr)
		}
	}
}

// writeFile writes content to path, creating its parents.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
