package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

func TestSelectResolvesABareNameUnderTheConfigHome(t *testing.T) {
	dir := t.TempDir()
	home := config.NewHome(dir)

	got, err := config.Select(home, "acme")
	if err != nil {
		t.Fatalf("Select() err = %v, want nil", err)
	}
	want := filepath.Join(dir, "blueprints", "acme.yaml")
	if got != want {
		t.Errorf("Select() = %q, want %q", got, want)
	}
}

func TestSelectRejectsAnEmptyName(t *testing.T) {
	if _, err := config.Select(config.NewHome(t.TempDir()), ""); err == nil {
		t.Fatal("Select() err = nil, want an error for an empty blueprint name")
	}
}

func TestLoadReadsAndParsesTheSelectedBlueprint(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\nrepos:\n  - url: git@github.com:acme/api.git\n")

	got, err := config.Load(config.NewHome(dir), "acme")
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if got.Access != "tailscale" || len(got.Repos) != 1 {
		t.Errorf("Load() = %+v, want access tailscale and one repo", got)
	}
}

func TestLoadReportsAMissingBlueprintWithThePathItLookedFor(t *testing.T) {
	dir := t.TempDir()

	_, err := config.Load(config.NewHome(dir), "staging")
	if err == nil {
		t.Fatal("Load() err = nil, want an error for a blueprint that does not exist")
	}
	want := filepath.Join(dir, "blueprints", "staging.yaml")
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("Load() error = %q, want it to name %q", got, want)
	}
}

func TestLoadCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	before := tree(t, dir)

	if _, err := config.Load(config.NewHome(dir), "acme"); err == nil {
		t.Fatal("Load() err = nil, want an error for an absent config home")
	}
	if after := tree(t, dir); !equal(before, after) {
		t.Errorf("config home = %v after Load(), want it unchanged at %v", after, before)
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

// tree lists every path under dir, relative to it, so a test can assert that a
// read-only command left the config home exactly as it found it.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(dir, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return paths
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
