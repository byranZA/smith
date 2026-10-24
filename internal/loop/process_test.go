package loop_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
)

func TestProcessStreamsTheAgentsOutputWithItsEnvironmentAdded(t *testing.T) {
	t.Setenv("SMITH_INHERITED", "kept")
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", `echo "$SMITH_INHERITED $SMITH_ADDED"`}, Env: []string{"SMITH_ADDED=added"}}

	if err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(context.Background(), cmd); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	if got, want := out.String(), "kept added\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProcessRunsTheAgentInItsDirectory(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "pwd -P"}, Dir: dir}

	if err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(context.Background(), cmd); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("Launch() ran in %q, want %q", got, want)
	}
}

func TestProcessReportsAnAgentThatFailsAsOneThatStarted(t *testing.T) {
	t.Parallel()
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "exit 3"}}

	if err := (loop.Process{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Launch(t.Context(), cmd); err == nil || errors.Is(err, loop.ErrNotStarted) {
		t.Errorf("Launch(exit 3) error = %v, want the failed exit reported, not ErrNotStarted", err)
	}
}

func TestProcessReportsAnAgentThatCannotStart(t *testing.T) {
	t.Parallel()
	cmd := agent.Command{Name: "smith-no-such-agent"}

	if err := (loop.Process{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Launch(t.Context(), cmd); !errors.Is(err, loop.ErrNotStarted) {
		t.Errorf("Launch(%q) error = %v, want ErrNotStarted", cmd.Name, err)
	}
}

func TestProcessGivesTheTerminalsInputOnlyToAnAttachedAgent(t *testing.T) {
	mise := standInMise(t)
	tests := []struct {
		name     string
		mise     string
		attached bool
		want     string
	}{
		{"attached", "", true, "steer\n"},
		{"unattended", "", false, ""},
		{"attached under mise", mise, true, "steer\n"},
		{"unattended under mise", mise, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			p := loop.Process{Stdin: strings.NewReader("steer\n"), Stdout: &out, Stderr: &out, Mise: tt.mise}

			if err := p.Launch(context.Background(), agent.Command{Name: "cat", Attached: tt.attached}); err != nil {
				t.Fatalf("Launch() error = %v", err)
			}

			if got := out.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// pinningMise is a stand-in mise that runs the command after exec's "--" in the directory --cd names, with the env a worktree's config pins.
const pinningMise = `#!/bin/sh
[ "$1" = --cd ] && cd "$2" && shift 2
[ "$1" = exec ] && [ "$2" = -- ] || exit 64
shift 2
export SMITH_PINNED=from-mise
exec "$@"
`

// standInMise writes pinningMise to a temp dir and returns its path.
func standInMise(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mise")
	if err := os.WriteFile(path, []byte(pinningMise), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// launchReporting runs an agent in dir under p that prints what it sees of report, and returns its output.
func launchReporting(t *testing.T, p loop.Process, dir, report string, env ...string) string {
	t.Helper()
	var out bytes.Buffer
	p.Stdout, p.Stderr = &out, &out
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo " + report}, Dir: dir, Env: env}
	if err := p.Launch(t.Context(), cmd); err != nil {
		t.Fatalf("Launch() error = %v (output %q)", err, out.String())
	}
	return strings.TrimSpace(out.String())
}

// realTempDir is a temp dir with its symlinks resolved, as pwd -P reports it.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProcessRunsTheAgentUnderMiseInItsWorktree(t *testing.T) {
	t.Parallel()
	dir := realTempDir(t)

	got := launchReporting(t, loop.Process{Mise: standInMise(t)}, dir, `"$SMITH_PINNED $(pwd -P)"`)

	if want := "from-mise " + dir; got != want {
		t.Errorf("the agent saw %q, want %q", got, want)
	}
}

func TestProcessUnderMiseTrustsTheWorktree(t *testing.T) {
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", "")
	dir := realTempDir(t)

	got := launchReporting(t, loop.Process{Mise: standInMise(t)}, dir, `"$MISE_TRUSTED_CONFIG_PATHS"`)

	if got != dir {
		t.Errorf("the agent saw trusted paths %q, want %q", got, dir)
	}
}

func TestProcessUnderMiseKeepsTheTrustSmithInherited(t *testing.T) {
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", "/home/op/code")
	dir := realTempDir(t)

	got := launchReporting(t, loop.Process{Mise: standInMise(t)}, dir, `"$MISE_TRUSTED_CONFIG_PATHS"`)

	if want := "/home/op/code:" + dir; got != want {
		t.Errorf("the agent saw trusted paths %q, want %q", got, want)
	}
}

func TestProcessUnderMiseKeepsTheAgentsEnvironment(t *testing.T) {
	t.Parallel()

	got := launchReporting(t, loop.Process{Mise: standInMise(t)}, realTempDir(t), `"$SMITH_ADDED"`, "SMITH_ADDED=added")

	if got != "added" {
		t.Errorf("the agent saw SMITH_ADDED=%q, want %q", got, "added")
	}
}
