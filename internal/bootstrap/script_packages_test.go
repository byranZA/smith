package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptPackagesInstallWhatSmithRunsOnAMinimalImage(t *testing.T) {
	t.Parallel()
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
