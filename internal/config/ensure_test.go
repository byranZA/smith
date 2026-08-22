package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// readFile returns the file's content, failing the test if it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

// modeOf returns the permission bits of the directory or file at path.
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return info.Mode().Perm()
}

func TestEnsureHomeCreatesTheHomeAndCacheDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")
	home := config.NewHome(dir)

	if err := config.EnsureHome(home); err != nil {
		t.Fatalf("EnsureHome() err = %v, want nil", err)
	}
	if got := modeOf(t, dir); got != 0o700 {
		t.Errorf("config home mode = %v, want 0700", got)
	}
	if got := modeOf(t, home.CachePath()); got != 0o700 {
		t.Errorf("cache directory mode = %v, want 0700", got)
	}
}

func TestEnsureHomeIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")
	home := config.NewHome(dir)

	if err := config.EnsureHome(home); err != nil {
		t.Fatalf("first EnsureHome() err = %v, want nil", err)
	}
	first := readFile(t, filepath.Join(dir, ".gitignore"))
	if err := config.EnsureHome(home); err != nil {
		t.Fatalf("second EnsureHome() err = %v, want nil", err)
	}
	if got := readFile(t, filepath.Join(dir, ".gitignore")); got != first {
		t.Errorf("gitignore after a second call = %q, want it unchanged at %q", got, first)
	}
}

func TestEnsureHomeGitignoresTheCacheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")

	if err := config.EnsureHome(config.NewHome(dir)); err != nil {
		t.Fatalf("EnsureHome() err = %v, want nil", err)
	}
	if got := readFile(t, filepath.Join(dir, ".gitignore")); !strings.Contains(got, "cache/") {
		t.Errorf("gitignore = %q, want it to list the cache directory", got)
	}
}

func TestEnsureHomeAppendsToAnExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("scratch/\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := config.EnsureHome(config.NewHome(dir)); err != nil {
		t.Fatalf("EnsureHome() err = %v, want nil", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "scratch/") {
		t.Errorf("gitignore = %q, want the operator's own lines preserved", got)
	}
	if !strings.Contains(got, "cache/") {
		t.Errorf("gitignore = %q, want it to also list the cache directory", got)
	}
}

func TestEnsureHomeLeavesAGitignoreAlreadyListingTheCacheUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const content = "scratch/\ncache/\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := config.EnsureHome(config.NewHome(dir)); err != nil {
		t.Fatalf("EnsureHome() err = %v, want nil", err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("gitignore = %q, want it byte-identical at %q", got, content)
	}
}

func TestEnsureHomeAppendsOnItsOwnLineWhenTheGitignoreHasNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("scratch/"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := config.EnsureHome(config.NewHome(dir)); err != nil {
		t.Fatalf("EnsureHome() err = %v, want nil", err)
	}
	if got := readFile(t, path); !strings.Contains(got, "scratch/\n") {
		t.Errorf("gitignore = %q, want the appended entry on its own line", got)
	}
}
