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

func TestProcessHoldsBackAHeldCommandsOutputOnSuccess(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo activity; echo more >&2"}, HoldOutput: true}

	if err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd); err != nil {
		t.Fatalf("Launch(%q %q) error = %v", cmd.Name, cmd.Args, err)
	}

	if out.Len() != 0 {
		t.Errorf("Launch(%q %q) with HoldOutput wrote %q, want none", cmd.Name, cmd.Args, out.String())
	}
}

func TestProcessShowsAHeldCommandsOutputOnFailure(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo activity; echo more >&2; exit 3"}, HoldOutput: true}

	err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd)

	if err == nil {
		t.Fatalf("Launch(%q %q) error = nil, want a failure", cmd.Name, cmd.Args)
	}
	if got := out.String(); got != "activity\nmore\n" && got != "more\nactivity\n" {
		t.Errorf("Launch(%q %q) with HoldOutput wrote %q, want both lines \"activity\" and \"more\"", cmd.Name, cmd.Args, got)
	}
}

func TestProcessCutsAHeldCommandsOutputOverTheBound(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "i=0; while [ $i -lt 5000 ]; do echo line-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done; exit 1"}, HoldOutput: true}

	_ = (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd)

	got := out.String()
	if !strings.HasPrefix(got, "smith: earlier agent output was cut; showing the last 64 KiB\n") {
		t.Errorf("Launch(5000-line command) with HoldOutput wrote a start of %q, want the cut note %q", got[:min(len(got), 80)], "smith: earlier agent output was cut; showing the last 64 KiB\n")
	}
	if !strings.HasSuffix(got, "line-4999-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n") || strings.Contains(got, "line-0-") {
		t.Errorf("Launch(5000-line command) with HoldOutput wrote a tail of %q, want it to end with line-4999 and not contain line-0", got[max(0, len(got)-80):])
	}
}

func TestProcessKeepsAnAttachedCommandOnTheTerminalDespiteHoldOutput(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo shown"}, Attached: true, HoldOutput: true}

	if err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd); err != nil {
		t.Fatalf("Launch(%q %q) error = %v", cmd.Name, cmd.Args, err)
	}

	if got := out.String(); got != "shown\n" {
		t.Errorf("Launch(%q %q) with Attached and HoldOutput wrote %q, want %q", cmd.Name, cmd.Args, got, "shown\n")
	}
}

func TestProcessSucceedsSilentlyWhenAHeldCommandExitsCleanlyLeavingABackgroundProcess(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo activity; sleep 5 & exit 0"}, HoldOutput: true}

	err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd)

	if err != nil {
		t.Errorf("Launch(%q %q) error = %v, want nil", cmd.Name, cmd.Args, err)
	}
	if out.Len() != 0 {
		t.Errorf("Launch(%q %q) with HoldOutput wrote %q, want none", cmd.Name, cmd.Args, out.String())
	}
}

func TestProcessFailsWhenAHeldCommandExitsNonZeroLeavingABackgroundProcess(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo activity; sleep 5 & exit 3"}, HoldOutput: true}

	err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(t.Context(), cmd)

	if err == nil {
		t.Errorf("Launch(%q %q) error = nil, want a failure", cmd.Name, cmd.Args)
	}
	if got, want := out.String(), "activity\n"; got != want {
		t.Errorf("Launch(%q %q) with HoldOutput wrote %q, want %q", cmd.Name, cmd.Args, got, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestProcessKeepsAKilledHeldCommandKilledWhenShowingItsOutputFails(t *testing.T) {
	t.Parallel()
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "echo activity; kill -9 $$"}, HoldOutput: true}

	err := (loop.Process{Stdout: failingWriter{}, Stderr: failingWriter{}}).Launch(t.Context(), cmd)

	if !errors.Is(err, loop.ErrKilled) {
		t.Errorf("Launch() error = %v, want it to be ErrKilled", err)
	}
}
