package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// fakeStagingBox stands in for a box reached over ssh during the staging stage:
// it records every remote command and the stdin each one was handed, and no
// test here reaches a real box.
type fakeStagingBox struct {
	commands []string
	inputs   []string
}

func (f *fakeStagingBox) Run(_ context.Context, cmd string, _, _ io.Writer) error {
	f.commands = append(f.commands, cmd)
	return nil
}

func (f *fakeStagingBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	f.commands = append(f.commands, cmd)
	f.inputs = append(f.inputs, string(data))
	return nil
}

func TestStageConfigStagesTheNamedBlueprintByteForByte(t *testing.T) {
	dir := t.TempDir()
	document := "# acme\naccess: tailscale\nterminal: tmux\n"
	writeBlueprint(t, dir, "acme", document)
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	if len(box.inputs) != 1 || box.inputs[0] != document {
		t.Errorf("staged bytes = %q, want the blueprint byte for byte, %q", box.inputs, document)
	}
	if !strings.Contains(out.String(), "/etc/smith/blueprint.yaml") {
		t.Errorf("stageConfig() reported %q, want it to name the staged document", out.String())
	}
}

func TestStageConfigStagesNothingWithoutABlueprint(t *testing.T) {
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(t.TempDir()), "", &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want nothing staged", box.commands)
	}
}

func TestStageConfigRefusesAnInvalidBlueprintBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "terminals: tmux\n")
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out); err == nil {
		t.Fatal("stageConfig() err = nil, want an invalid blueprint refused")
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want it untouched", box.commands)
	}
}

func TestSetupTakesTheBlueprintToStage(t *testing.T) {
	cmd := newSetupCmd(func() (config.Home, error) { return config.NewHome(t.TempDir()), nil })
	if cmd.Flags().Lookup("blueprint") == nil {
		t.Error("machine setup has no --blueprint flag, so no box can be told what kind of box it is")
	}
}
