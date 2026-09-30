//go:build unix

package starter_test

import (
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/starter"
)

// cappedHomeEnv names the config home a re-executed test binary scaffolds
// under a file size cap. Its presence is what makes the helper test run.
const cappedHomeEnv = "SMITH_STARTER_CAPPED_HOME"

// scaffoldUnderFileSizeCap runs Scaffold on the config home at dir in a child
// test process whose writes are capped, so a starter write fails partway
// through as it would on a full disk. The cap is process-wide, so it is kept
// out of this process, where it would also fail the test harness's own log
// writes.
func scaffoldUnderFileSizeCap(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperScaffoldUnderFileSizeCap$") // #nosec G204 -- re-executes this test binary.
	cmd.Env = append(os.Environ(), cappedHomeEnv+"="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("capped Scaffold in child process: %v\n%s", err, out)
	}
}

// TestHelperScaffoldUnderFileSizeCap is the child process body of
// scaffoldUnderFileSizeCap and does nothing when run directly. The kernel
// signals a write past the cap, so the signal is ignored for the write to
// return its error instead.
func TestHelperScaffoldUnderFileSizeCap(t *testing.T) {
	dir := os.Getenv(cappedHomeEnv)
	if dir == "" {
		t.Skip("runs only as the child process of scaffoldUnderFileSizeCap")
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatalf("get file size limit: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	limit.Cur = 64
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatalf("limit file size: %v", err)
	}

	home := config.NewHome(dir)
	_, err := starter.Scaffold(home, blueprint.Git{})
	if err == nil || !strings.Contains(err.Error(), home.PreferencesPath()) {
		t.Fatalf("Scaffold() err = %v, want an error naming %s", err, home.PreferencesPath())
	}
}

func TestScaffoldCompletesAStarterWhoseWriteFailedOnRetry(t *testing.T) {
	dir := t.TempDir()
	home := config.NewHome(dir)
	want := readFile(t, scaffold(t, t.TempDir())[0].Path)

	scaffoldUnderFileSizeCap(t, dir)
	got := scaffold(t, dir)

	if !got[0].Created {
		t.Errorf("retry outcome %+v, want the starter preferences created", got[0])
	}
	if content := readFile(t, home.PreferencesPath()); content != want {
		t.Errorf("preferences after retry = %q, want the complete starter", content)
	}
	if _, err := config.LoadPreferences(home); err != nil {
		t.Errorf("LoadPreferences() after retry err = %v, want nil", err)
	}
}
