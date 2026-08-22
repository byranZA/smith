package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/provider"
)

// sshConnectExitCode is ssh's exit status for a connection that never
// established, the status the connection package classifies as unreachable.
const sshConnectExitCode = 255

// refusedExit is the process error a refused ssh reports, carrying the exit
// status the connection package reads.
type refusedExit struct{}

func (refusedExit) Error() string { return "exit status 255" }

func (refusedExit) ExitCode() int { return sshConnectExitCode }

// refusingSSH stands in for the local ssh binary: it records the destination
// every launch was pointed at and refuses the connection, which is enough to
// see which target a verb resolved its argument to without a box existing.
type refusingSSH struct {
	targets []string
}

// Run records the ssh destination and refuses to connect.
func (s *refusingSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	if name == "ssh" && len(args) >= 2 {
		s.targets = append(s.targets, args[len(args)-2])
	}
	return refusedExit{}
}

// first returns the destination the first ssh launch was pointed at.
func (s *refusingSSH) first(t *testing.T) string {
	t.Helper()
	if len(s.targets) == 0 {
		t.Fatal("no ssh connection was opened, want one")
	}
	return s.targets[0]
}

// runMachine runs a machine subcommand against a config home rooted at dir,
// with every ssh launch pointed at the given fake, and returns what landed on
// each stream plus the exit code.
func runMachine(t *testing.T, dir string, ssh *refusingSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// devInventory registers one box under a bare name, the shape every resolution
// case here is decided against.
const devInventory = `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`

func TestMachineStatusConnectsToWhatARegisteredNameResolvesTo(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, _, _ = runMachine(t, dir, ssh, "status", "dev")

	if got := ssh.first(t); got != "smith@100.92.14.7" {
		t.Errorf("ssh target = %q, want the target the inventory maps dev to", got)
	}
}

func TestMachineStatusPrefersTheInventoryOverAnIdenticallyNamedSSHConfigAlias(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, _, _ = runMachine(t, dir, ssh, "status", "dev")

	if got := ssh.first(t); got == "dev" {
		t.Errorf("ssh target = %q, want the inventory's target rather than the name ssh would resolve itself", got)
	}
}

func TestMachineStatusConnectsVerbatimToALiteralTarget(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, _, code := runMachine(t, dir, ssh, "status", "root@203.0.113.10")

	if got := ssh.first(t); got != "root@203.0.113.10" {
		t.Errorf("ssh target = %q, want the literal target connected to verbatim", got)
	}
	if code == 1 {
		t.Errorf("exit code = 1, want a <login>@<host> argument to be connected to rather than rejected")
	}
}

func TestMachineStatusHandsAnUnknownBareNameToSSHVerbatim(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, _, _ = runMachine(t, dir, ssh, "status", "myalias")

	if got := ssh.first(t); got != "myalias" {
		t.Errorf("ssh target = %q, want the unknown name handed to ssh as written", got)
	}
}

func TestMachineStatusNamesBothFixesWhenAnUnregisteredBareArgumentIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	stdout, stderr, code := runMachine(t, dir, ssh, "status", "203.0.113.10")

	out := stdout + stderr
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a box smith could not reach (output: %s)", out)
	}
	for _, want := range []string{"203.0.113.10 is not a registered box", "smith@203.0.113.10", "smith machine add"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to name %q", out, want)
		}
	}
}

func TestMachineStatusSuggestsNothingWhenTheNameWasRegistered(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	stdout, stderr, _ := runMachine(t, dir, ssh, "status", "dev")

	if out := stdout + stderr; strings.Contains(out, "smith machine add") {
		t.Errorf("output = %q, want no registration advice for a box that is already registered", out)
	}
}

func TestMachineSetupConnectsToWhatARegisteredNameResolvesTo(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, _, _ = runMachine(t, dir, ssh, "setup", "dev")

	if got := ssh.first(t); got != "smith@100.92.14.7" {
		t.Errorf("ssh target = %q, want a registered name to re-run against its target", got)
	}
}

func TestMachineSetupStillNamesTheTargetShapeForAnUnknownBareArgument(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, devInventory)
	ssh := &refusingSSH{}

	_, stderr, code := runMachine(t, dir, ssh, "setup", "nosuchbox")

	if code == 0 {
		t.Fatal("exit code = 0, want a refusal for an argument that is neither a registered name nor a target")
	}
	if !strings.Contains(stderr, "<login>@<host>") {
		t.Errorf("stderr = %q, want the refusal to name the <login>@<host> shape", stderr)
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want no connection opened", ssh.targets)
	}
}

// TestMachineStatusPrependsNothingToABareArgument keeps the deleted convention
// deleted: status no longer has a private rule that makes a bare argument mean
// the smith user.
func TestMachineStatusPrependsNothingToABareArgument(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{}}`)
	ssh := &refusingSSH{}

	_, _, _ = runMachine(t, dir, ssh, "status", "203.0.113.10")

	if got := ssh.first(t); strings.HasPrefix(got, "smith@") {
		t.Errorf("ssh target = %q, want nothing prepended to a bare argument", got)
	}
}
