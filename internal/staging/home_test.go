package staging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/secret"
)

// operatorHomeWithToken makes a temp dir the operator's home, holding
// .secrets/gh-token, and returns it.
func operatorHomeWithToken(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".secrets"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".secrets", "gh-token"), []byte("ghp_fromhome"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return home
}

func TestResolveStagesAnEnvValueReadThroughAHomeRelativePath(t *testing.T) {
	operatorHomeWithToken(t)
	root := t.TempDir()
	b := blueprint.Blueprint{Env: map[string]string{"GITHUB_TOKEN": "file:~/.secrets/gh-token"}}

	tree, err := Resolve(Plan([]byte("access: public\n"), b, Resolution{}), secret.Resolve, blueprint.Value)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	stageEnv(t, root, tree)

	got, err := ReadValue(root, "", "GITHUB_TOKEN", hint.Invocation{})
	if err != nil {
		t.Fatalf("ReadValue() error = %v", err)
	}
	if got != "ghp_fromhome" {
		t.Errorf("staged GITHUB_TOKEN = %q, want %q", got, "ghp_fromhome")
	}
}

func TestResolveStagesAPlacementSourcedThroughAHomeRelativePath(t *testing.T) {
	operatorHomeWithToken(t)
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "file:~/.secrets/gh-token", To: "~/.config/gh/token"}}}

	tree, err := Resolve(Plan(nil, b, Resolution{}), secret.Resolve, blueprint.Value)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := string(tree.Placements[0].File.Bytes); got != "ghp_fromhome" {
		t.Errorf("staged bytes = %q, want %q", got, "ghp_fromhome")
	}
}

func TestResolveRefusesAMissingHomeRelativePathNamingItAsWrittenAndExpanded(t *testing.T) {
	home := operatorHomeWithToken(t)
	b := blueprint.Blueprint{Env: map[string]string{"TOKEN": "file:~/.secrets/missing"}}

	_, err := Resolve(Plan([]byte("access: public\n"), b, Resolution{}), secret.Resolve, blueprint.Value)
	if err == nil {
		t.Fatal("Resolve() error = nil, want the missing file refused")
	}
	for _, want := range []string{"file:~/.secrets/missing", filepath.Join(home, ".secrets", "missing")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error = %v, want it to name %q", err, want)
		}
	}
}
