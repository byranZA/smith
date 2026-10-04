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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	interrupted := make(chan bool, 1)
	go func() { interrupted <- interruptOnceWritten(ctx, cancel, pidfile) }()

	start := time.Now()
	err := (loop.Process{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Launch(ctx, cmd)
	elapsed := time.Since(start)
	cancel()

	if !<-interrupted {
		t.Fatalf("Launch() = %v after %v; the agent never wrote its child's pid", err, elapsed)
	}
	if err == nil || elapsed > 5*time.Second || !gone(t, pidfile) {
		t.Errorf("Launch() = %v after %v; want an error, soon, with the agent's child stopped too", err, elapsed)
	}
}

// interruptOnceWritten calls cancel once path is written, reporting whether it did before ctx ended.
func interruptOnceWritten(ctx context.Context, cancel context.CancelFunc, path string) bool {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
			if exists(path) {
				cancel()
				return true
			}
		}
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
