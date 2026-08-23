package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/staging"
)

// fakeStagingBox stands in for a box reached over ssh during the staging
// stage, so no test here reaches a real one.
//
// It is deliberately thin: what the staging stage does to a box is
// internal/staging's own behavior, tested there against a box of its own. What
// is tested here is the wiring — that the command surface reads the operator's
// blueprint, resolves it, hands it to the stage, and maps a refusal to the
// exit code the setup family answers it with — so this box answers every read
// with one canned reply and either takes a write or rejects it.
type fakeStagingBox struct {
	// reply is what the box answers any read with. The only read the wiring
	// makes is of the marker recording the blueprint the box was built from.
	reply string
	// writeErr rejects every write, standing in for a box that fails partway
	// through the staging stage.
	writeErr error

	commands []string
	inputs   []string
}

func (f *fakeStagingBox) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	f.commands = append(f.commands, cmd)
	_, err := io.WriteString(stdout, f.reply)
	return err
}

func (f *fakeStagingBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	f.commands = append(f.commands, cmd)
	f.inputs = append(f.inputs, string(data))
	return f.writeErr
}

// stagedOrFatal resolves the named blueprint from a config home rooted at dir,
// as `machine setup` does before it touches the box, and fails the test if it
// will not resolve.
func stagedOrFatal(t *testing.T, dir, name string) *stagedConfig {
	t.Helper()
	staged, err := resolveStagedConfig(config.NewHome(dir), name, io.Discard)
	if err != nil {
		t.Fatalf("resolveStagedConfig(%q) err = %v, want it to resolve", name, err)
	}
	return staged
}

// gateRejection is the exit code the setup family answers a refusal that
// mutated nothing with, and whether the error carries one at all.
func gateRejection(err error) (int, bool) {
	var exit *exitError
	if !errors.As(err, &exit) {
		return 0, false
	}
	return exit.code, true
}

func TestSetupTakesTheBlueprintToStage(t *testing.T) {
	cmd := newSetupCmd(func() (config.Home, error) { return config.NewHome(t.TempDir()), nil }, connection.System())
	if cmd.Flags().Lookup("blueprint") == nil {
		t.Error("machine setup has no --blueprint flag, so no box can be told what kind of box it is")
	}
}

// TestSetupTakesTheBoxName pins the flag that names the box: without it the
// operator-chosen name lives only in the operator's inventory, so an entry
// rebuilt from the box comes back as an address.
func TestSetupTakesTheBoxName(t *testing.T) {
	cmd := newSetupCmd(func() (config.Home, error) { return config.NewHome(t.TempDir()), nil }, connection.System())
	flag := cmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("machine setup has no --name flag, so no box can record what it is called")
	}
	if flag.DefValue != "" {
		t.Errorf("--name default = %q, want empty: smith never invents a name", flag.DefValue)
	}
}

func TestStageConfigStagesTheNamedBlueprintByteForByte(t *testing.T) {
	dir := t.TempDir()
	document := "# acme\naccess: tailscale\nterminal: tmux\n"
	writeBlueprint(t, dir, "acme", document)
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, stagedOrFatal(t, dir, "acme"), &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	if len(box.inputs) == 0 || box.inputs[0] != document {
		t.Errorf("staged bytes = %q, want the blueprint byte for byte, %q", box.inputs, document)
	}
	if !strings.Contains(out.String(), staging.DocumentPath) {
		t.Errorf("stageConfig() reported %q, want it to name the staged document", out.String())
	}
}

func TestStageConfigStagesNothingWithoutABlueprint(t *testing.T) {
	box := &fakeStagingBox{}
	var out bytes.Buffer

	if err := stageConfig(context.Background(), box, stagedOrFatal(t, t.TempDir(), ""), &out); err != nil {
		t.Fatalf("stageConfig() err = %v, want nil", err)
	}
	if len(box.commands) != 0 {
		t.Errorf("stageConfig() ran %q on the box, want nothing staged", box.commands)
	}
}

func TestResolveStagedConfigResolvesEveryPlacementSourceOnTheOperatorsMachine(t *testing.T) {
	const credential = "//registry.npmjs.org/:_authToken=s3cr3t"
	t.Setenv("NPM_TOKEN", credential)
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: env:NPM_TOKEN\n    to: ~/.npmrc\n    perms: \"0640\"\n")

	staged := stagedOrFatal(t, dir, "acme")
	if len(staged.tree.Placements) != 1 {
		t.Fatalf("resolved %d placement(s), want the one the blueprint declares", len(staged.tree.Placements))
	}
	if got := string(staged.tree.Placements[0].File.Bytes); got != credential {
		t.Errorf("resolved bytes = %q, want the value the source names, %q", got, credential)
	}
}

func TestStageConfigNamesTheBlueprintTheBoxChokedOn(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: tailscale\n")
	staged := stagedOrFatal(t, dir, "acme")
	box := &fakeStagingBox{writeErr: errors.New("permission denied")}
	var out bytes.Buffer

	err := stageConfig(context.Background(), box, staged, &out)
	if err == nil {
		t.Fatal("stageConfig() err = nil, want a box that rejected a write reported")
	}
	if !strings.Contains(err.Error(), staged.path) {
		t.Errorf("stageConfig() err = %v, want it to name the blueprint the box choked on, %s", err, staged.path)
	}
}

func TestResolveStagedConfigRefusesAnInvalidBlueprintAsAGateRejection(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "terminals: tmux\n")
	var errOut bytes.Buffer

	staged, err := resolveStagedConfig(config.NewHome(dir), "acme", &errOut)
	if err == nil {
		t.Fatal("resolveStagedConfig() err = nil, want an invalid blueprint refused")
	}
	if staged != nil {
		t.Errorf("resolveStagedConfig() = %v, want nothing to stage on a refusal", staged)
	}
	if code, ok := gateRejection(err); !ok || code != 2 {
		t.Errorf("resolveStagedConfig() err = %v, want a gate rejection (exit 2)", err)
	}
	if !strings.Contains(errOut.String(), "terminals") {
		t.Errorf("resolveStagedConfig() reported %q, want it to name the unknown field", errOut.String())
	}
}

func TestResolveStagedConfigRefusesAnUnresolvableSourceAsAGateRejection(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "placements:\n  - from: file:"+dir+"/missing\n    to: /home/smith/.npmrc\n")
	var errOut bytes.Buffer

	staged, err := resolveStagedConfig(config.NewHome(dir), "acme", &errOut)
	if err == nil {
		t.Fatal("resolveStagedConfig() err = nil, want the unresolvable source refused")
	}
	if staged != nil {
		t.Errorf("resolveStagedConfig() = %v, want nothing to stage on a refusal", staged)
	}
	if code, ok := gateRejection(err); !ok || code != 2 {
		t.Errorf("resolveStagedConfig() err = %v, want a gate rejection (exit 2)", err)
	}
	if !strings.Contains(errOut.String(), dir+"/missing") {
		t.Errorf("resolveStagedConfig() reported %q, want it to name the unresolvable reference", errOut.String())
	}
}

func TestSetupReportsABlueprintPointerRefusalAsAGateRejection(t *testing.T) {
	var errOut bytes.Buffer

	err := refuseSetup(&errOut, staging.Pointer(marker.Marker{Blueprint: "acme"}, ""))
	if err == nil {
		t.Fatal("refuseSetup() err = nil, want a box built from a blueprint to refuse a blueprint-less re-run")
	}
	if code, ok := gateRejection(err); !ok || code != 2 {
		t.Errorf("refuseSetup() err = %v, want a gate rejection (exit 2)", err)
	}
	if !strings.Contains(errOut.String(), "acme") {
		t.Errorf("refuseSetup() reported %q, want it to name the blueprint the box was built from", errOut.String())
	}
}
