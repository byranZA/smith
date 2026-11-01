package misetrust_test

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/byranZA/smith/internal/misetrust"
)

// runCommand runs Command for the stand-in mise with environ as its whole environment and returns what it printed.
func runCommand(t *testing.T, dir string, environ []string, args ...string) string {
	t.Helper()
	argv := misetrust.Command(mise, dir, args...)
	cmd := exec.CommandContext(t.Context(), argv[0], argv[1:]...)
	cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, environ...)
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("run %v: %v: %s", argv, err, exitErr.Stderr)
	}
	if err != nil {
		t.Fatalf("run %v: %v", argv, err)
	}
	return string(out)
}

func TestCommandRunsMiseWithItsArgumentsTrustingTheDirectory(t *testing.T) {
	t.Parallel()

	got := runCommand(t, "/srv/workspace/smith", nil, "--cd", "/srv/workspace/smith/worktrees/spec-42", "install")

	if want := "/srv/workspace/smith|[--cd][/srv/workspace/smith/worktrees/spec-42][install]"; got != want {
		t.Errorf("Command() ran mise printing %q, want %q", got, want)
	}
}

func TestCommandKeepsTheTrustTheEnvironmentAlreadyGrants(t *testing.T) {
	t.Parallel()

	got := runCommand(t, "/srv/workspace/smith", []string{"MISE_TRUSTED_CONFIG_PATHS=/home/op/code:/opt/shared"}, "install")

	if want := "/home/op/code:/opt/shared:/srv/workspace/smith|[install]"; got != want {
		t.Errorf("Command() ran mise printing %q, want %q", got, want)
	}
}

func TestCommandPassesArgumentsThroughUnsplit(t *testing.T) {
	t.Parallel()

	got := runCommand(t, "/srv/workspace/my repo", nil, "exec", "--", "sh", "-c", "npm ci && make")

	if want := "/srv/workspace/my repo|[exec][--][sh][-c][npm ci && make]"; got != want {
		t.Errorf("Command() ran mise printing %q, want %q", got, want)
	}
}
