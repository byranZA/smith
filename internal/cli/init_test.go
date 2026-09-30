package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
)

// runInit runs `smith init` against a config home rooted at dir and returns
// what landed on each stream plus the process exit code.
func runInit(t *testing.T, dir string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newInitCmd(func() (config.Home, error) { return config.NewHome(dir), nil }, connection.System())
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

// globalGitConfig points git's global config at a file holding content, so a
// test decides what identity the operator's machine has.
func globalGitConfig(t *testing.T, content string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	path := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, path, content)
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestInitDeclaresTheGlobalGitIdentity(t *testing.T) {
	globalGitConfig(t, "[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n")
	// A repository's own identity in the working directory is not the
	// operator's, so init must not pick it up.
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", repo},
		{"-C", repo, "config", "user.name", "Repo Bot"},
		{"-C", repo, "config", "user.email", "bot@example.com"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	t.Chdir(repo)
	dir := t.TempDir()

	stdout, stderr, code := runInit(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences() err = %v, want nil", err)
	}
	if want := (blueprint.Git{UserName: "Ada Lovelace", UserEmail: "ada@example.com"}); got.Declared.Git != want {
		t.Errorf("preferences git = %+v, want the global %+v", got.Declared.Git, want)
	}
	if !strings.Contains(stdout, "git identity taken from the global git config") {
		t.Errorf("stdout = %q, want it to say the identity came from the global git config", stdout)
	}
	if _, stderr, code := runCheck(t, dir, "check"); code != 0 {
		t.Errorf("blueprint check exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
}

func TestInitDeclaresNoIdentityWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()

	stdout, stderr, code := runInit(t, dir)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 without git (stderr: %s)", code, stderr)
	}
	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences() err = %v, want nil", err)
	}
	if got.Declared.Git != (blueprint.Git{}) {
		t.Errorf("preferences git = %+v, want no identity", got.Declared.Git)
	}
	if strings.Contains(stdout, "git identity") {
		t.Errorf("stdout = %q, want no identity reported", stdout)
	}
}

func TestInitLeavesExistingPreferencesWithoutAnIdentity(t *testing.T) {
	globalGitConfig(t, "[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n")
	dir := t.TempDir()
	prefs := filepath.Join(dir, "preferences.yaml")
	writeFile(t, prefs, "access: public\n")

	stdout, _, _ := runInit(t, dir)

	if data, err := os.ReadFile(prefs); err != nil || string(data) != "access: public\n" {
		t.Errorf("preferences = %q (err %v), want them unchanged", data, err)
	}
	if strings.Contains(stdout, "git identity") {
		t.Errorf("stdout = %q, want no identity reported for preferences left alone", stdout)
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
