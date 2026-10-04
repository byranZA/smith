//go:build !windows

package loop_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
)

func TestProcessStopsTheAgentAndWhatItStartedWhenInterrupted(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	cmd := agent.Command{Name: "sh", Args: []string{"-c", `sleep 30 & echo $! > "$0"; wait`, pidfile}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for !exists(pidfile) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()

	start := time.Now()
	err := (loop.Process{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Launch(ctx, cmd)

	if err == nil || time.Since(start) > 10*time.Second || !gone(t, pidfile) {
		t.Errorf("Launch() = %v after %v; want an error, soon, with the agent's child stopped too", err, time.Since(start))
	}
}

func exists(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.HasSuffix(string(b), "\n")
}

// gone reports whether the process whose pid is in pidfile has ended within a few seconds.
func gone(t *testing.T, pidfile string) bool {
	t.Helper()
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}
