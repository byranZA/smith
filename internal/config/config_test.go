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

	got, path, err := config.Load(config.NewHome(dir), "acme")
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if got.Access != "tailscale" || len(got.Repos) != 1 {
		t.Errorf("Load() = %+v, want access tailscale and one repo", got)
	}
	if want := filepath.Join(dir, "blueprints", "acme.yaml"); path != want {
		t.Errorf("Load() path = %q, want the file it read, %q", path, want)
	}
}

func TestLoadReportsAMissingBlueprintWithThePathItLookedFor(t *testing.T) {
	dir := t.TempDir()

	_, _, err := config.Load(config.NewHome(dir), "staging")
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

	if _, _, err := config.Load(config.NewHome(dir), "acme"); err == nil {
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

func TestSelectTreatsAValueCarryingAPathMarkerAsAPath(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{value: "./acme.yaml", want: "./acme.yaml"},
		{value: "../shared/acme.yaml", want: "../shared/acme.yaml"},
		{value: "~/blueprints/a.yaml", want: "~/blueprints/a.yaml"},
		{value: "/etc/acme.yaml", want: "/etc/acme.yaml"},
		{value: "team/acme.yaml", want: "team/acme.yaml"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := config.Select(config.NewHome(t.TempDir()), tc.value)
			if err != nil {
				t.Fatalf("Select(%q) err = %v, want nil", tc.value, err)
			}
			if got != tc.want {
				t.Errorf("Select(%q) = %q, want it used verbatim as %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestSelectRefusesABareNameCarryingAnExtension(t *testing.T) {
	_, err := config.Select(config.NewHome(t.TempDir()), "acme.yaml")
	if err == nil {
		t.Fatal("Select() err = nil, want a refusal for a name carrying an extension")
	}
	got := err.Error()
	if !strings.Contains(got, "extension") {
		t.Errorf("Select() error = %q, want it to say the extension is not part of a blueprint name", got)
	}
	if !strings.Contains(got, `"acme"`) {
		t.Errorf("Select() error = %q, want it to name %q as what to type instead", got, "acme")
	}
}

func TestLoadReadsABlueprintPathVerbatim(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "shared.yaml")
	if err := os.WriteFile(outside, []byte("access: public\n"), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	// A blueprint of the same name inside the config home must not be read.
	writeBlueprint(t, dir, "shared", "access: tailscale\n")

	got, path, err := config.Load(config.NewHome(dir), outside)
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if got.Access != "public" {
		t.Errorf("Load() access = %q, want %q from the file the path names", got.Access, "public")
	}
	if path != outside {
		t.Errorf("Load() path = %q, want the path it was given, %q", path, outside)
	}
}

func TestLoadReportsAMissingBlueprintPathByPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "shared.yaml")

	_, _, err := config.Load(config.NewHome(t.TempDir()), missing)
	if err == nil {
		t.Fatal("Load() err = nil, want an error for a blueprint path that does not exist")
	}
	got := err.Error()
	if !strings.Contains(got, missing) {
		t.Errorf("Load() error = %q, want it to name %q", got, missing)
	}
	if strings.Contains(got, "no blueprint named") {
		t.Errorf("Load() error = %q, want it to report a path rather than a name", got)
	}
}

func TestLoadPreferencesReportsAnAbsentFileWithoutCreatingIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent-home")

	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences() err = %v, want nil for an absent config home", err)
	}
	if got.Found {
		t.Error("LoadPreferences() found = true, want false for an absent config home")
	}
	if want := filepath.Join(dir, "preferences.yaml"); got.Path != want {
		t.Errorf("LoadPreferences() path = %q, want %q", got.Path, want)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("config home exists after LoadPreferences(), want it not created")
	}
}

func TestLoadPreferencesTreatsAnAbsentFileAsUnsetPreferences(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent-home")

	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences() err = %v, want nil for an absent config home", err)
	}
	if got.Declared.Access != "" || got.Declared.Workspace != "" {
		t.Errorf("LoadPreferences() = %+v, want every field unset", got.Declared)
	}
}

func TestLoadPreferencesReadsAndParsesThePreferences(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "access: tailscale\nworkspace: /srv/work\n")

	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences() err = %v, want nil", err)
	}
	if got.Declared.Access != "tailscale" || got.Declared.Workspace != "/srv/work" {
		t.Errorf("LoadPreferences() = %+v, want access tailscale and workspace /srv/work", got.Declared)
	}
	if !got.Found {
		t.Error("LoadPreferences() found = false, want true for preferences in the config home")
	}
	if want := filepath.Join(dir, "preferences.yaml"); got.Path != want {
		t.Errorf("LoadPreferences() path = %q, want the file it read, %q", got.Path, want)
	}
}

func TestLoadPreferencesReportsAnInvalidFileWithItsPath(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, "repos:\n  - url: git@github.com:acme/api.git\n")

	_, err := config.LoadPreferences(config.NewHome(dir))
	if err == nil {
		t.Fatal("LoadPreferences() err = nil, want a collection in preferences refused")
	}
	got := err.Error()
	if !strings.Contains(got, filepath.Join(dir, "preferences.yaml")) {
		t.Errorf("LoadPreferences() error = %q, want it to name the preferences file", got)
	}
	if !strings.Contains(got, "repos") {
		t.Errorf("LoadPreferences() error = %q, want it to name the refused field", got)
	}
}

func writePreferences(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "preferences.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write preferences: %v", err)
	}
}
