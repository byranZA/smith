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

func TestScriptSwapIsAQuarterOfFreeDiskOnASmallDisk(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	env = append(env, "FREE_DISK_KB=2097152") // 2 GB free

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	fi, err := os.Stat(filepath.Join(dir, "swapfile"))
	if err != nil {
		t.Fatalf("stat swapfile: %v", err)
	}
	if got, want := fi.Size(), int64(512<<20); got != want {
		t.Errorf("swapfile size = %d, want %d (512 MB, a quarter of 2 GB free)", got, want)
	}
	assertSwapCompleteAndPackagesFollow(t, dir, out)
}

func TestScriptSwapSkipsForLackOfDisk(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	env = append(env, "FREE_DISK_KB=819200") // 800 MB free

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	assertNoSwapfileOrRebootEntry(t, dir)
	if !strings.Contains(out, "swap skipped: not enough free disk") {
		t.Errorf("setup output does not report swap skipped for lack of disk:\n%s", out)
	}
	assertSwapCompleteAndPackagesFollow(t, dir, out)
}

func TestScriptSwapLeavesExistingSwapUntouched(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	swaps := procSwapsHeader + "/dev/zram0\tpartition\t1048572\t\t0\t\t100\n"
	fstab := "UUID=abcd / ext4 defaults 0 1\n"
	writeTestFile(t, filepath.Join(dir, "swaps"), swaps)
	writeTestFile(t, filepath.Join(dir, "fstab"), fstab)

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(dir, "swapfile")); !os.IsNotExist(err) {
		t.Errorf("a swapfile was added beside existing swap (stat err = %v)", err)
	}
	if got := readFile(t, filepath.Join(dir, "swaps")); got != swaps {
		t.Errorf("active swap list changed:\ngot  %q\nwant %q", got, swaps)
	}
	if got := readFile(t, filepath.Join(dir, "fstab")); got != fstab {
		t.Errorf("fstab changed:\ngot  %q\nwant %q", got, fstab)
	}
	assertSwapCompleteAndPackagesFollow(t, dir, out)
}

func TestScriptSwapRefusedLeavesNothingBehind(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	env = append(env, "SWAPON_REFUSED=1")
	// A reboot entry left by an earlier attempt goes too; the rest of fstab stays.
	const rootEntry = "UUID=abcd / ext4 defaults 0 1\n"
	writeTestFile(t, filepath.Join(dir, "fstab"), rootEntry+filepath.Join(dir, "swapfile")+" none swap sw 0 0\n")

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	assertNoSwapfileOrRebootEntry(t, dir)
	if got := readFile(t, filepath.Join(dir, "fstab")); got != rootEntry {
		t.Errorf("fstab = %q, want only the entries smith did not add, %q", got, rootEntry)
	}
	if !strings.Contains(out, "swap skipped: not supported on this box") {
		t.Errorf("setup output does not report swap as unsupported:\n%s", out)
	}
	assertSwapCompleteAndPackagesFollow(t, dir, out)
}

// assertNoSwapfileOrRebootEntry fails the test when the swapfile exists or fstab
// carries an entry for it.
func assertNoSwapfileOrRebootEntry(t *testing.T, dir string) {
	t.Helper()
	swapfile := filepath.Join(dir, "swapfile")
	if _, err := os.Stat(swapfile); !os.IsNotExist(err) {
		t.Errorf("swapfile left behind (stat err = %v)", err)
	}
	if fstab, err := os.ReadFile(filepath.Join(dir, "fstab")); err == nil && strings.Contains(string(fstab), swapfile) {
		t.Errorf("fstab carries a reboot entry for the swapfile:\n%s", fstab)
	}
}

// assertSwapCompleteAndPackagesFollow fails the test unless the marker records swap
// as complete and setup's output out shows it went on to the packages phase.
func assertSwapCompleteAndPackagesFollow(t *testing.T, dir, out string) {
	t.Helper()
	m, _, err := decodeMarker(t, filepath.Join(dir, "bootstrap.json"))
	if err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if len(m.CompletedPhases) < 2 || m.CompletedPhases[0] != "swap" || m.CompletedPhases[1] != "packages" {
		t.Errorf("marker CompletedPhases = %v, want swap complete then packages", m.CompletedPhases)
	}
	if !strings.Contains(out, "▶ packages") {
		t.Errorf("setup did not go on to packages:\n%s", out)
	}
}

// writeTestFile writes contents to path, failing the test when it cannot.
func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScriptSwapRetryAfterAFailedRebootEntryWriteLeavesSwapPersistent(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	swapfile := filepath.Join(dir, "swapfile")
	fstabPath := filepath.Join(dir, "fstab")
	// An fstab that is a directory cannot be written, so the first attempt fails.
	if err := os.Mkdir(fstabPath, 0o755); err != nil {
		t.Fatalf("mkdir fstab: %v", err)
	}

	if out, err := runSetup(bash, scriptPath, env); err == nil {
		t.Fatalf("setup run succeeded with an unwritable fstab:\n%s", out)
	}
	if err := os.Remove(fstabPath); err != nil {
		t.Fatalf("remove fstab: %v", err)
	}
	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("retried setup run failed: %v\n%s", err, out)
	}

	if got := strings.Count(readFile(t, filepath.Join(dir, "swaps")), swapfile); got != 1 {
		t.Errorf("active swap list carries the swapfile %d times after a retry, want 1", got)
	}
	if got, want := readFile(t, fstabPath), swapfile+" none swap sw 0 0\n"; got != want {
		t.Errorf("fstab = %q, want exactly the reboot entry %q", got, want)
	}
}

func TestScriptSwapRerunRepairsAMissingRebootEntryForSmithsSwapfile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	swapfile := filepath.Join(dir, "swapfile")
	const rootEntry = "UUID=abcd / ext4 defaults 0 1\n"
	writeTestFile(t, filepath.Join(dir, "swaps"), procSwapsHeader+swapfile+"\tfile\t\t2097148\t\t0\t\t-2\n")
	writeTestFile(t, filepath.Join(dir, "fstab"), rootEntry)

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	if got, want := readFile(t, filepath.Join(dir, "fstab")), rootEntry+swapfile+" none swap sw 0 0\n"; got != want {
		t.Errorf("fstab = %q, want the reboot entry restored, %q", got, want)
	}
	if !strings.Contains(out, "✓ swap\n") {
		t.Errorf("setup output does not report swap as changed after the repair:\n%s", out)
	}
}
