//go:build !windows

package connection_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

func TestRunCancelledKillsTheProcessesTheCommandStarted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	pidFile := filepath.Join(t.TempDir(), "pid")
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- connection.System().Run(ctx, "sh", []string{"-c", "sleep 60 & echo $! > " + pidFile + "; wait"}, nil, &out, &out)
	}()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		raw, err := os.ReadFile(pidFile)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read the pid file: %v", err)
		}
		// A partly written file does not parse yet; the next poll reads it whole.
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			pid = n
		}
	}
	if pid == 0 {
		t.Fatal("the command never reported its child's pid")
	}

	cancel()
	if err := <-done; err == nil {
		t.Fatal("Run() = nil, want an error after cancellation")
	}

	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d is still alive after Run was cancelled", pid)
}
