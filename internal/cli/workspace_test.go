package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/staging"
)

// fakeBox stands in for the box the workspace stage runs on: it records the
// argv of every command and answers the package probe as a box that holds
// nothing installed.
type fakeBox struct {
	calls [][]string
	err   error
}

func (f *fakeBox) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if strings.Contains(strings.Join(args, " "), "apt-get") {
		return f.err
	}
	return errors.New("no packages found matching")
}

// line returns the argv of the one apt-get invocation, joined for readability.
func (f *fakeBox) line(t *testing.T) string {
	t.Helper()
	var found []string
	for _, argv := range f.calls {
		if line := strings.Join(argv, " "); strings.Contains(line, "apt-get") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("apt-get ran %d times, want 1: %v", len(found), f.calls)
	}
	return found[0]
}

// staged answers with the blueprint a box holds staged.
func staged(b blueprint.Blueprint) stagedResolver {
	return func() (blueprint.Blueprint, error) { return b, nil }
}

// runWorkspace drives the assembled workspace command the way an operator
// does, with every boundary faked, and returns what landed on each stream plus
// the exit code.
func runWorkspace(t *testing.T, w workspaceWiring, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newWorkspaceCmd(w)
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// TestWorkspaceConvergeInstallsDeclaredPackages proves the verb run on the box
// reads what was staged there, installs the packages the blueprint declares,
// and reports what it did.
func TestWorkspaceConvergeInstallsDeclaredPackages(t *testing.T) {
	box := &fakeBox{}
	w := workspaceWiring{
		blueprint: staged(blueprint.Blueprint{Packages: []string{"ripgrep", "jq"}}),
		command:   box,
	}
	stdout, stderr, code := runWorkspace(t, w, "converge")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if line := box.line(t); !strings.Contains(line, "install -y ripgrep jq") {
		t.Errorf("apt-get invocation = %q, want it to install the declared packages", line)
	}
	if !strings.Contains(stdout, "packages") {
		t.Errorf("stdout = %q, want the step reported", stdout)
	}
}

// TestWorkspaceConvergeRefusesWithNothingStaged proves the verb inherits the
// staged-config refusal rather than redefining one: the exit code the absent
// document is owed, and a message naming the command that stages it.
func TestWorkspaceConvergeRefusesWithNothingStaged(t *testing.T) {
	box := &fakeBox{}
	w := workspaceWiring{
		blueprint: func() (blueprint.Blueprint, error) {
			return blueprint.Blueprint{}, &staging.AbsentError{Path: staging.DocumentPath}
		},
		command: box,
	}
	cmd := newWorkspaceCmd(w)
	cmd.SetArgs([]string{"converge"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()

	if err == nil || !strings.Contains(err.Error(), "machine setup") {
		t.Errorf("refusal = %v, want it to name machine setup", err)
	}
	if code := codeFromError(err); code != exitStagedBlueprintAbsent {
		t.Errorf("exit code = %d, want %d", code, exitStagedBlueprintAbsent)
	}
	if len(box.calls) != 0 {
		t.Errorf("a refused converge ran %v, want nothing", box.calls)
	}
}

// TestWorkspaceConvergeFailsOnAFailedStep proves a step apt refused exits
// non-zero and says what failed, rather than reporting a converged box.
func TestWorkspaceConvergeFailsOnAFailedStep(t *testing.T) {
	box := &fakeBox{err: relayExit(100)}
	w := workspaceWiring{
		blueprint: staged(blueprint.Blueprint{Packages: []string{"jq"}}),
		command:   box,
	}
	stdout, _, code := runWorkspace(t, w, "converge")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "failed") {
		t.Errorf("stdout = %q, want it to report the failed step", stdout)
	}
}

// TestWorkspaceConvergeWithABoxRunsOnTheBox proves the verb follows the one
// rule every relayed verb shares: naming a box sends it there over ssh, and
// nothing about the box's packages is decided on the operator's machine.
func TestWorkspaceConvergeWithABoxRunsOnTheBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &fakeSSHRelay{stdout: "workspace: 1 step converged\n"}
	box := &fakeBox{}
	w := workspaceWiring{
		blueprint: staged(blueprint.Blueprint{Packages: []string{"jq"}}),
		home:      func() (config.Home, error) { return config.NewHome(dir), nil },
		command:   box,
		ssh:       ssh,
		version:   "0.2.0",
	}

	stdout, stderr, code := runWorkspace(t, w, "converge", "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	line := ssh.line(t)
	for _, want := range []string{"smith@100.92.14.7", "/usr/local/bin/smith", "--relayed-from '0.2.0'", "'workspace' 'converge'"} {
		if !strings.Contains(line, want) {
			t.Errorf("ssh argv = %q, want it to contain %q", line, want)
		}
	}
	if len(box.calls) != 0 {
		t.Errorf("a relayed converge ran %v on this machine, want nothing", box.calls)
	}
	if !strings.Contains(stdout, "1 step converged") {
		t.Errorf("stdout = %q, want the box's own output", stdout)
	}
}
