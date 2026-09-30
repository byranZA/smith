//go:build unix

package starter_test

import (
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/starter"
)

// limitFileSize caps how many bytes this process may write into any file, so
// a starter write fails partway through as it would on a full disk. The kernel
// signals a write past the cap, so the signal is ignored for the write to
// return its error instead. Both are restored when the test ends.
func limitFileSize(t *testing.T, limit uint64) {
	t.Helper()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatalf("get file size limit: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	capped := original
	capped.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &capped); err != nil {
		t.Fatalf("limit file size: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Errorf("restore file size limit: %v", err)
		}
		signal.Reset(syscall.SIGXFSZ)
	})
}

func TestScaffoldCompletesAStarterWhoseWriteFailedOnRetry(t *testing.T) {
	dir := t.TempDir()
	home := config.NewHome(dir)
	want := readFile(t, scaffold(t, t.TempDir())[0].Path)

	t.Run("the write fails", func(t *testing.T) {
		limitFileSize(t, 64)
		_, err := starter.Scaffold(home, blueprint.Git{})
		if err == nil || !strings.Contains(err.Error(), home.PreferencesPath()) {
			t.Fatalf("Scaffold() err = %v, want an error naming %s", err, home.PreferencesPath())
		}
	})

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
