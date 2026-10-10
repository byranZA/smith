package bootstrap

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tmuxScopeDropin is what the tmux-oom-policy phase writes: a scope drop-in that keeps a tmux pane's scope running when the kernel OOM-kills one of its processes.
const tmuxScopeDropin = "[Scope]\nOOMPolicy=continue\n"

func TestScriptTmuxOOMPolicyWritesTheTmuxScopeDropin(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)

	if out, err := runSetup(bash, scriptPath, env); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	dropin := filepath.Join(dir, "systemd-user", "tmux-spawn-.scope.d", "50-smith-oom-policy.conf")
	if got := readFile(t, dropin); got != tmuxScopeDropin {
		t.Errorf("tmux scope drop-in = %q, want %q", got, tmuxScopeDropin)
	}
}

func TestScriptTmuxOOMPolicyReloadsEveryRunningUserManager(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	env = append(env, "USER_MANAGERS=user@0.service user@1000.service")

	if out, err := runSetup(bash, scriptPath, env); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	want := "--kill-whom=main --signal=SIGHUP user@0.service\n" +
		"--kill-whom=main --signal=SIGHUP user@1000.service\n"
	if got := readFile(t, filepath.Join(dir, "systemctl.kill.log")); got != want {
		t.Errorf("systemctl kill calls = %q, want a reload signal to each running user manager %q", got, want)
	}
}

func TestScriptTmuxOOMPolicyRerunIsASatisfiedNoOp(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, scriptPath, env := scriptFixture(t)
	env = append(env, "USER_MANAGERS=user@1000.service")
	killLog := filepath.Join(dir, "systemctl.kill.log")

	if out, err := runSetup(bash, scriptPath, env); err != nil {
		t.Fatalf("first setup run failed: %v\n%s", err, out)
	}
	if err := os.Remove(killLog); err != nil {
		t.Fatalf("clear systemctl kill log: %v", err)
	}
	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("second setup run failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "✓ tmux-oom-policy (already-satisfied)") {
		t.Errorf("re-run output does not report tmux-oom-policy already-satisfied:\n%s", out)
	}
	if _, err := os.Stat(killLog); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("re-run signalled a user manager (stat %s: %v), want no reload", killLog, err)
	}
}

func TestScriptTmuxOOMPolicySkipsWithANoteOnASystemdTooOldForScopeOOMPolicy(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	for _, version := range []string{"249", "252"} {
		t.Run("systemd "+version, func(t *testing.T) {
			t.Parallel()
			dir, scriptPath, env := scriptFixture(t)
			env = append(env, "SYSTEMD_VERSION="+version, "USER_MANAGERS=user@1000.service")

			out, err := runSetup(bash, scriptPath, env)
			if err != nil {
				t.Fatalf("setup run with systemd %s failed: %v\n%s", version, err, out)
			}

			note := "  tmux-oom-policy skipped: systemd " + version + " does not support OOMPolicy= on scopes (it needs 253 or later)\n"
			if !strings.Contains(out, note+"✓ tmux-oom-policy (already-satisfied)\n") {
				t.Errorf("setup output with systemd %s does not carry the note %q before the phase's result:\n%s", version, note, out)
			}
			dropin := filepath.Join(dir, "systemd-user", "tmux-spawn-.scope.d", "50-smith-oom-policy.conf")
			if _, err := os.Stat(dropin); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("stat tmux scope drop-in with systemd %s: %v, want it absent", version, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "systemctl.kill.log")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("setup with systemd %s signalled a user manager (%v), want no reload", version, err)
			}
		})
	}
}

func TestScriptTmuxOOMPolicyRunsAfterPackages(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	_, scriptPath, env := scriptFixture(t)

	out, err := runSetup(bash, scriptPath, env)
	if err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}

	packagesDone := strings.Index(out, "✓ packages")
	tmuxStart := strings.Index(out, "▶ tmux-oom-policy")
	if packagesDone < 0 || tmuxStart < 0 || packagesDone > tmuxStart {
		t.Errorf("setup output: packages must complete before tmux-oom-policy starts:\n%s", out)
	}
}
