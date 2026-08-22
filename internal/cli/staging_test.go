package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/staging"
)

// fakeStagingBox stands in for a box reached over ssh during the staging stage:
// it records every remote command and the stdin each one was handed, and no
// test here reaches a real box.
type fakeStagingBox struct {
	staged []string
	// marker is the JSON the box answers a marker read with, standing in for a
	// box that records the blueprint it was built from.
	marker string
	// writeErrOn is the path whose write the box rejects, standing in for a
	// staging run that fails partway through.
	writeErrOn string

	commands []string
	inputs   []string
}

func (f *fakeStagingBox) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	f.commands = append(f.commands, cmd)
	if strings.Contains(cmd, staging.MarkerPath) {
		if _, err := io.WriteString(stdout, f.marker); err != nil {
			return err
		}
	}
	if strings.Contains(cmd, "find") {
		for _, path := range f.staged {
			if _, err := io.WriteString(stdout, path+"\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *fakeStagingBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	f.commands = append(f.commands, cmd)
	f.inputs = append(f.inputs, string(data))
	if f.writeErrOn != "" && strings.Contains(cmd, f.writeErrOn) {
		return errors.New("permission denied")
	}
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

func TestStageConfigStagesABoxPlacementsBytesOverStdin(t *testing.T) {
	const credential = "//registry.npmjs.org/:_authToken=s3cr3t"
	t.Setenv("NPM_TOKEN", credential)
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: env:NPM_TOKEN\n    to: ~/.npmrc\n    perms: \"0640\"\n")
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	var delivered bool
	for _, in := range box.inputs {
		if in == credential {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("stdin carried %q, want the resolved credential among it", box.inputs)
	}
	for _, cmd := range box.commands {
		if strings.Contains(cmd, credential) {
			t.Errorf("the credential reached a command line: %q", cmd)
		}
	}
	if !strings.Contains(out.String(), staging.BoxPlacementPathIn(staging.Root, "/home/smith/.npmrc")) {
		t.Errorf("stageConfig() reported %q, want it to name the staged placement", out.String())
	}
}

func TestStageConfigRefusesAnUnresolvableSourceBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: file:"+dir+"/missing\n    to: /home/smith/.npmrc\n")
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out); err == nil {
		t.Fatal("stageConfig() err = nil, want the unresolvable source refused")
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want it untouched", box.commands)
	}
}

func TestStageConfigReportsWhatItPruned(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	stray := staging.PlacementsDir + "/left-by-a-human"
	box := &fakeStagingBox{staged: []string{stray}}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "pruned") || !strings.Contains(out.String(), stray) {
		t.Errorf("stageConfig() reported %q, want it to report %s as pruned", out.String(), stray)
	}
}

func TestStageConfigRefusesAnUnsetEnvSourceBeforeTouchingTheBox(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: env:NPM_TOKEN_UNSET\n    to: /home/smith/.npmrc\n")
	box := &fakeStagingBox{}
	var out bytes.Buffer

	err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out)
	if err == nil {
		t.Fatal("stageConfig() err = nil, want the unset variable refused")
	}
	if !strings.Contains(err.Error(), "NPM_TOKEN_UNSET") {
		t.Errorf("stageConfig() err = %v, want it to name the unset variable", err)
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want it untouched", box.commands)
	}
}

func TestStageConfigEnumeratesEveryUnresolvableSource(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme",
		"placements:\n  - from: file:"+dir+"/missing\n    to: /home/smith/.npmrc\n"+
			"  - from: env:NPM_TOKEN_UNSET\n    to: /home/smith/.netrc\n")
	box := &fakeStagingBox{}
	var out bytes.Buffer

	err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out)
	if err == nil {
		t.Fatal("stageConfig() err = nil, want both unresolvable sources refused")
	}
	for _, want := range []string{dir + "/missing", "NPM_TOKEN_UNSET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("stageConfig() err = %v, want one refusal naming %q", err, want)
		}
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want it untouched", box.commands)
	}
}

func TestStageConfigNamesTheWriteThatFailedPartwayThrough(t *testing.T) {
	t.Setenv("NPM_TOKEN", "s3cr3t")
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: env:NPM_TOKEN\n    to: /home/smith/.npmrc\n")
	placement := staging.BoxPlacementPathIn(staging.Root, "/home/smith/.npmrc")
	box := &fakeStagingBox{writeErrOn: placement}
	var out bytes.Buffer

	err := stageConfig(context.Background(), box, config.NewHome(dir), "acme", &out)
	if err == nil {
		t.Fatal("stageConfig() err = nil, want the failed write reported")
	}
	if !strings.Contains(err.Error(), placement) {
		t.Errorf("stageConfig() err = %v, want it to name the write that failed", err)
	}
}

func TestCheckBlueprintPointerRefusesABlueprintlessRerun(t *testing.T) {
	box := &fakeStagingBox{marker: `{"schema_version":1,"access_mode":"public","blueprint":"acme"}`}
	var errOut bytes.Buffer

	err := checkBlueprintPointer(context.Background(), box, "", &errOut)
	if err == nil {
		t.Fatal("checkBlueprintPointer() err = nil, want a box built from a blueprint to refuse a blueprint-less re-run")
	}
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Errorf("checkBlueprintPointer() err = %v, want a gate rejection (exit 2)", err)
	}
	if !strings.Contains(errOut.String(), "acme") {
		t.Errorf("checkBlueprintPointer() reported %q, want it to name the blueprint the box was built from", errOut.String())
	}
	for _, cmd := range box.commands {
		if !strings.Contains(cmd, staging.MarkerPath) {
			t.Errorf("checkBlueprintPointer() ran %q on the box, want the refusal to leave the staged config untouched", cmd)
		}
	}
}

func TestCheckBlueprintPointerAllowsABoxThatRecordsNoBlueprint(t *testing.T) {
	box := &fakeStagingBox{marker: `{"schema_version":1,"access_mode":"public","blueprint":""}`}
	var errOut bytes.Buffer

	if err := checkBlueprintPointer(context.Background(), box, "", &errOut); err != nil {
		t.Fatalf("checkBlueprintPointer() err = %v, want a box with no pointer to be set up with no blueprint", err)
	}
	if errOut.String() != "" {
		t.Errorf("checkBlueprintPointer() reported %q, want nothing said", errOut.String())
	}
}
