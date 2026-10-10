package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptSwapGivesABoxWithoutSwapA2GBSwapfile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	swapfile := filepath.Join(dir, "swapfile")

	if out, err := runSetup(bash, scriptPath, env); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	fi, err := os.Stat(swapfile)
	if err != nil {
		t.Fatalf("stat swapfile: %v", err)
	}
	if got, want := fi.Size(), int64(2<<30); got != want {
		t.Errorf("swapfile size = %d, want %d (2 GB)", got, want)
	}
	if got, want := fi.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("swapfile mode = %v, want %v (readable only by root)", got, want)
	}
	swaps := readFile(t, filepath.Join(dir, "swaps"))
	if !strings.Contains(swaps, swapfile+"\t") {
		t.Errorf("active swap list does not carry the swapfile %s:\n%s", swapfile, swaps)
	}
	fstab := readFile(t, filepath.Join(dir, "fstab"))
	if want := swapfile + " none swap sw 0 0\n"; fstab != want {
		t.Errorf("fstab = %q, want the reboot entry %q", fstab, want)
	}
}

func TestScriptSwapRunsBeforePackages(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	_, scriptPath, env := scriptFixture(t)

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	swapDone := strings.Index(out, "✓ swap")
	packagesStart := strings.Index(out, "▶ packages")
	if swapDone < 0 || packagesStart < 0 || swapDone > packagesStart {
		t.Errorf("setup output: swap must complete before packages starts:\n%s", out)
	}
}

func TestScriptSwapRerunReportsSwapPresentWithoutDuplicating(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	swapfile := filepath.Join(dir, "swapfile")

	if out, err := runSetup(bash, scriptPath, env); err != nil {
		t.Fatalf("first setup run failed: %v\n%s", err, out)
	}
	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("second setup run failed: %v\n%s", err, out)
	}

	for _, want := range []string{"swap already present", "✓ swap (already-satisfied)"} {
		if !strings.Contains(out, want) {
			t.Errorf("re-run output missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(readFile(t, filepath.Join(dir, "swaps")), swapfile); got != 1 {
		t.Errorf("active swap list carries the swapfile %d times after a re-run, want 1", got)
	}
	if got := strings.Count(readFile(t, filepath.Join(dir, "fstab")), swapfile); got != 1 {
		t.Errorf("fstab carries the swapfile %d times after a re-run, want 1", got)
	}
}

// runSetup runs the script's setup subcommand in public mode and returns its combined output.
func runSetup(bash, scriptPath string, env []string) (string, error) {
	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// readFile returns the contents of path, failing the test when it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
