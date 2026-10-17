package session_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// gitDirOf returns the absolute git dir of the worktree at dir.
func gitDirOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		t.Fatalf("git rev-parse --absolute-git-dir in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

// openRecord opens the session record of the worktree at dir.
func openRecord(t *testing.T, dir string) session.Record {
	t.Helper()
	record, err := session.OpenRecord(context.Background(), connection.System(), dir)
	if err != nil {
		t.Fatalf("OpenRecord(%q) err = %v", dir, err)
	}
	return record
}

// leftovers returns the temporary record files left in gitDir.
func leftovers(t *testing.T, gitDir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(gitDir, "smith-session.*"))
	if err != nil {
		t.Fatalf("Glob in %s: %v", gitDir, err)
	}
	return got
}

func TestSessionListStaysUsableWhileARunRecordsItsOutcome(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	record := openRecord(t, startLoop(t, env))
	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		for {
			select {
			case <-done:
				return
			default:
			}
			if err := record.Conclude("complete"); err != nil {
				errs <- err
				return
			}
		}
	}()

	for range 200 {
		if _, err := session.List(context.Background(), env, session.Filter{}); err != nil {
			t.Errorf("List() while a run records its outcome err = %v, want none", err)
			break
		}
	}
	close(done)
	if err := <-errs; err != nil {
		t.Fatalf("Conclude() err = %v", err)
	}
}

func TestAFailedConcludeKeepsThePreviousRecord(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	dir := startLoop(t, env)
	conclude(t, dir, "cap reached")
	gitDir := gitDirOf(t, dir)
	if err := os.Chmod(gitDir, 0o500); err != nil {
		t.Fatalf("Chmod(%q) err = %v", gitDir, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(gitDir, 0o700); err != nil {
			t.Errorf("Chmod(%q) err = %v", gitDir, err)
		}
	})

	if err := openRecord(t, dir).Conclude("complete"); err == nil {
		t.Errorf("Conclude() into a read-only git dir err = nil, want an error")
	}
	if got := listed(t, env, "smith-spec-42"); got.Outcome != "cap reached" {
		t.Errorf("outcome after a failed Conclude = %q, want %q", got.Outcome, "cap reached")
	}
}

func TestAFailedConcludeLeavesNoTemporaryFile(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	dir := startLoop(t, env)
	gitDir := gitDirOf(t, dir)
	recordPath := filepath.Join(gitDir, "smith-session")
	if err := os.Remove(recordPath); err != nil {
		t.Fatalf("Remove(%q) err = %v", recordPath, err)
	}
	if err := os.MkdirAll(filepath.Join(recordPath, "blocker"), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) err = %v", recordPath, err)
	}

	err := openRecord(t, dir).Conclude("complete")

	if err == nil || !strings.Contains(err.Error(), recordPath) {
		t.Errorf("Conclude() over a directory err = %v, want an error naming %s", err, recordPath)
	}
	if got := leftovers(t, gitDir); len(got) != 0 {
		t.Errorf("temporary files after a failed Conclude = %v, want none", got)
	}
}
