//go:build !windows

package procgroup_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/procgroup"
)

func TestStopKillsWhatTheCancelledCommandStarted(t *testing.T) {
	t.Parallel()
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 30 & echo $! > "$0"; wait`, pidfile)
	procgroup.Stop(cmd)
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b, err := os.ReadFile(pidfile)
		if err == nil && strings.HasSuffix(string(b), "\n") {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
	}
	if pid == 0 {
		t.Fatal("the command never wrote its child's pid")
	}

	cancel()
	_ = cmd.Wait()

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	t.Errorf("child %d still running after the command was cancelled", pid)
}
