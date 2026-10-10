package cli

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/connection"
)

// runSessionAs runs a session verb relayed to box, or typed on the box when box is empty, returning stderr and its error.
func runSessionAs(t *testing.T, w sessionWiring, box string, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "smith", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String(relayedBoxFlag, "", "")
	root.AddCommand(newSessionCmd(w))
	line := append([]string{"session"}, args...)
	if box != "" {
		line = append([]string{"--" + relayedBoxFlag, box}, line...)
	}
	root.SetArgs(line)
	var errBuf bytes.Buffer
	root.SetOut(io.Discard)
	root.SetErr(&errBuf)
	err := root.Execute()
	return errBuf.String(), err
}

func TestAttachToAStoppedSessionNamesTheBoxTheOperatorTyped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		box  string
		want string
	}{
		{"relayed to a named box", "smith-dev", "resume it with `smith session start smith-dev --repo smith --branch try`"},
		{"relayed to a literal target", "smith@10.0.0.4", "resume it with `smith session start smith@10.0.0.4 --repo smith --branch try`"},
		{"typed on the box", "", "resume it with `smith session start --repo smith --branch try`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			workspace := t.TempDir()
			writeBareRepo(t, workspace, "smith")
			resolve := resolvedBox(workspace, "smith")
			tmux := &tmuxBox{}
			standUp(t, resolve, tmux, "smith", "try")
			stopSession(t, resolve, tmux, "smith-try")

			stderr, _ := runSessionAs(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), tt.box, "attach", "smith-try")

			if !strings.Contains(stderr, tt.want) {
				t.Errorf("session attach smith-try relayed as %q: stderr = %q, want it to contain %q", tt.box, stderr, tt.want)
			}
		})
	}
}

func TestATypedVerbOnAnUnstagedMachineSaysToNameTheBox(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	w := onBox(t, stagedIn(root), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, err := runSessionAs(t, w, "", "start", "--repo", "smith", "--branch", "try")

	want := "if this is the operator's machine, name the box the command is for: `smith session start <box> --branch=try --repo=smith`"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("session start err = %v, want it to contain %q", err, want)
	}
}

func TestARelayedVerbOnAnUnstagedBoxNamesTheBoxToSetUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	w := onBox(t, stagedIn(root), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, err := runSessionAs(t, w, "smith-dev", "list")

	want := "no blueprint is staged on this box at " + root + "/blueprint.yaml: run `smith machine setup smith-dev` to stage one"
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("session list err = %v, want it to end %q", err, want)
	}
}

func TestRelayedBoxIsHiddenFromHelp(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	root.SetArgs([]string{"--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() err = %v", err)
	}
	if strings.Contains(out.String(), relayedBoxFlag) {
		t.Errorf("help = %q, want no mention of --%s", out.String(), relayedBoxFlag)
	}
}

func TestSetupRelaysTheWorkspaceStageNamingTheBoxAsTyped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "packages:\n  - ripgrep\n")
	ssh := &setupSSH{}

	_, stderr, code := runSetup(t, dir, ssh, "--blueprint", "acme", "root@203.0.113.10")
	if code != 0 {
		t.Fatalf("machine setup exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	want := "--relayed-box 'root@203.0.113.10' 'workspace' 'converge'"
	if !slices.ContainsFunc(ssh.commands, func(c string) bool { return strings.Contains(c, want) }) {
		t.Errorf("machine setup ran %q, want a workspace converge relayed with %q", ssh.commands, want)
	}
}
