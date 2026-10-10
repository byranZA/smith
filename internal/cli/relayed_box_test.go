package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/connection"
)

// runSessionAs runs a session verb under a root carrying the relay's box flag,
// the way on-box smith is handed a command: relayed to box, or typed on the box
// when box is empty. It returns stderr and the error the command ended with.
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
	root := t.TempDir()
	w := onBox(t, stagedIn(root), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, err := runSessionAs(t, w, "", "start", "--repo", "smith", "--branch", "try")

	want := "if this is the operator's machine, name the box the command is for: `smith session start <box> --branch=try --repo=smith`"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("session start err = %v, want it to contain %q", err, want)
	}
}

func TestARelayedVerbOnAnUnstagedBoxNamesTheBoxToSetUp(t *testing.T) {
	root := t.TempDir()
	w := onBox(t, stagedIn(root), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, err := runSessionAs(t, w, "smith-dev", "list")

	want := "no blueprint is staged on this box at " + root + "/blueprint.yaml: run `smith machine setup smith-dev` to stage one"
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("session list err = %v, want it to end %q", err, want)
	}
}

func TestRelayedBoxIsHiddenFromHelp(t *testing.T) {
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

func TestSetupRelaysTheWorkspaceStageNamingTheBox(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "packages:\n  - ripgrep\n")
	ssh := &fakeSSHRelay{}
	run := &pipelineRun{
		accessMode:   "public",
		host:         "203.0.113.7",
		exec:         ssh,
		box:          "smith-dev",
		localVersion: "0.2.0",
		staged:       stagedOrFatal(t, dir, "acme"),
	}

	var workspace func(context.Context) error
	for _, stage := range run.stages(io.Discard, io.Discard) {
		if stage.Name == "workspace" {
			workspace = stage.Run
		}
	}
	if err := workspace(context.Background()); err != nil {
		t.Fatalf("workspace stage err = %v", err)
	}

	if line := ssh.line(t); !strings.Contains(line, "--relayed-box 'smith-dev' 'workspace' 'converge'") {
		t.Errorf("ssh argv = %q, want the stage to name the box as the operator did", line)
	}
}
