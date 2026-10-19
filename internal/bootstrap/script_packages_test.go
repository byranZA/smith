package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScriptPackagesInstallWhatSmithRunsOnAMinimalImage runs setup in public
// mode, the access mode that adds no packages of its own, and checks the
// packages phase installs what the rest of smith runs on: git for every repo
// and worktree step, tmux as the terminal substrate, and curl with
// ca-certificates for fetching mise's installer over HTTPS, and sudo for the
// smith user's passwordless-sudo grant. A minimal image such as Docker's
// ubuntu:24.04 ships none of them.
func TestScriptPackagesInstallWhatSmithRunsOnAMinimalImage(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	installLog, err := os.ReadFile(filepath.Join(dir, "apt.install.log"))
	if err != nil {
		t.Fatalf("read apt install log: %v", err)
	}
	installed := map[string]bool{}
	for _, field := range strings.Fields(string(installLog)) {
		installed[field] = true
	}
	for _, want := range []string{"git", "tmux", "curl", "ca-certificates", "sudo"} {
		if !installed[want] {
			t.Errorf("public-mode setup did not install %q:\n%s", want, installLog)
		}
	}
}
