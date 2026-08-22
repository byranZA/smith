package cli

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// fakeProviderRunner stands in for the provider CLI: no test in this package
// launches doctl or hcloud, and none creates a real box.
type fakeProviderRunner struct {
	stdout string
	stderr string
	err    error

	name string
	args []string
	runs int
}

func (f *fakeProviderRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.name, f.args, f.runs = name, slices.Clone(args), f.runs+1
	if _, err := io.WriteString(stdout, f.stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, f.stderr); err != nil {
		return err
	}
	return f.err
}

const providerBlueprint = `provider:
  create: [hcloud, server, create, --name, "{{name}}", --type, cpx31, -o, json]
  extract:
    id: id
    ip: public_net.ipv4.ip
`

// runCreate runs `machine create` against a config home rooted at dir and the
// given fake provider CLI, returning what landed on each stream plus the exit
// code.
func runCreate(t *testing.T, dir string, runner *fakeProviderRunner, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(func() (config.Home, error) { return config.NewHome(dir), nil }, runner)
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestMachineCreateReportsTheBoxAndTheNextCommand(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 519823476123, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a created box (stderr: %s)", code, stderr)
	}
	want := []string{"server", "create", "--name", "dev", "--type", "cpx31", "-o", "json"}
	if runner.name != "hcloud" || !slices.Equal(runner.args, want) {
		t.Errorf("ran %q %q, want %q %q", runner.name, runner.args, "hcloud", want)
	}
	for _, fragment := range []string{"519823476123", "203.0.113.10", "smith machine setup root@203.0.113.10"} {
		if !strings.Contains(stdout, fragment) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, fragment)
		}
	}
}

func TestMachineCreateRunsOnlyTheProviderCommand(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	if _, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	// One command, the operator's own create template: create does not
	// provision, so nothing is run against the box itself.
	if runner.runs != 1 {
		t.Errorf("ran %d commands, want exactly the create template", runner.runs)
	}
}

func TestMachineCreateRefusesABlueprintWithNoProvider(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "access: public\n")
	runner := &fakeProviderRunner{}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero when no provider adapter is configured")
	}
	if runner.runs != 0 {
		t.Errorf("ran %d commands, want none when no adapter is configured", runner.runs)
	}
	if !strings.Contains(stderr, "provider") {
		t.Errorf("stderr = %q, want it to report that no provider adapter is configured", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed when the create is refused", stdout)
	}
}

// providerBlueprintWithSSHKey is the same adapter passing an ssh key reference
// the operator registered at the provider themselves.
const providerBlueprintWithSSHKey = `provider:
  create: [hcloud, server, create, --name, "{{name}}", --ssh-key, "{{ssh_key}}", -o, json]
  ssh_key: my-laptop
  extract:
    id: id
    ip: public_net.ipv4.ip
`

func TestMachineCreateNamesTheSSHKeyReferenceItPassed(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprintWithSSHKey)
	runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a created box (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "ssh key") || !strings.Contains(stdout, "my-laptop") {
		t.Errorf("stdout = %q, want it to name %q as the ssh key reference passed", stdout, "my-laptop")
	}
}

func TestMachineCreateStatesThatItPassedNoSSHKeyReference(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a created box (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "ssh key") || !strings.Contains(stdout, "none") {
		t.Errorf("stdout = %q, want it to state that no ssh key reference was passed", stdout)
	}
	// Absence is legal: it is reported, never warned about.
	if stderr != "" {
		t.Errorf("stderr = %q, want no warning about an absent ssh key reference", stderr)
	}
}

func TestMachineCreateNamesTheSSHKeyReferenceAsTheFirstSuspectOnARefusedConnect(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"a reference was passed", providerBlueprintWithSSHKey, "my-laptop"},
		{"none was passed", providerBlueprint, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeBlueprint(t, dir, "acme", tt.doc)
			runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

			stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 for a created box (stderr: %s)", code, stderr)
			}
			advice := stdout[strings.Index(stdout, "smith machine setup"):]
			if !strings.Contains(advice, "Permission denied (publickey)") {
				t.Errorf("stdout = %q, want the refused-connect failure named after the setup line", stdout)
			}
			if !strings.Contains(advice, tt.want) {
				t.Errorf("stdout = %q, want the refused-connect advice to name %q", stdout, tt.want)
			}
		})
	}
}
