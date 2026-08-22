package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

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

// fakeDialer stands in for the network at the port-22 readiness poll: no test
// in this package opens a real socket. It refuses a fixed number of dials
// before accepting, the shape a box that is still booting has.
type fakeDialer struct {
	refusals int

	dialed []string
}

func (d *fakeDialer) Dial(_ context.Context, address string) error {
	d.dialed = append(d.dialed, address)
	if len(d.dialed) <= d.refusals {
		return errors.New("connection refused")
	}
	return nil
}

// fakeClock drives the create command's polls without waiting: every wait it is
// asked for has already elapsed.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	fired := make(chan time.Time, 1)
	fired <- c.now
	return fired
}

// runCreate runs `machine create` against a config home rooted at dir and the
// given fake provider CLI, against a box whose port 22 answers at once.
func runCreate(t *testing.T, dir string, runner *fakeProviderRunner, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runCreateDialing(t, context.Background(), dir, runner, &fakeDialer{}, args...)
}

// runCreateDialing runs `machine create` with the readiness poll pointed at the
// given dialer, returning what landed on each stream plus the exit code.
func runCreateDialing(t *testing.T, ctx context.Context, dir string, runner *fakeProviderRunner, dialer *fakeDialer, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(func() (config.Home, error) { return config.NewHome(dir), nil }, runner, dialer, &fakeClock{})
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.ExecuteContext(ctx))
	return out.String(), errBuf.String(), code
}

func TestMachineCreateReportsABoxReachableOncePort22Answers(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 519823476123, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}
	dialer := &fakeDialer{refusals: 2}

	stdout, stderr, code := runCreateDialing(t, context.Background(), dir, runner, dialer, "create", "dev", "--blueprint", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a box that answered (stderr: %s)", code, stderr)
	}
	if len(dialer.dialed) != 3 || dialer.dialed[0] != "203.0.113.10:22" {
		t.Errorf("dialed %q, want the poll to retry port 22 at the box address until it answered", dialer.dialed)
	}
	if !strings.Contains(stdout, "reachable") {
		t.Errorf("stdout = %q, want it to report the box as reachable", stdout)
	}
}

func TestMachineCreateReportsABoxThatNeverBecameReachable(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 519823476123, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	stdout, stderr, code := runCreateDialing(t, context.Background(), dir, runner, &fakeDialer{refusals: 1000}, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when the box never became reachable")
	}
	for _, fragment := range []string{"519823476123", "203.0.113.10"} {
		if !strings.Contains(stderr, fragment) {
			t.Errorf("stderr = %q, want it to name %q so the operator can find the box", stderr, fragment)
		}
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want no setup line for a box that never answered", stdout)
	}
}

func TestMachineCreateInterruptedStillNamesTheBox(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	runner := &fakeProviderRunner{stdout: `{"id": 519823476123, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, stderr, code := runCreateDialing(t, ctx, dir, runner, &fakeDialer{refusals: 1000}, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when the create was interrupted")
	}
	for _, fragment := range []string{"519823476123", "203.0.113.10"} {
		if !strings.Contains(stderr, fragment) {
			t.Errorf("stderr = %q, want it to name %q the poll had reached", stderr, fragment)
		}
	}
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
	// An adapter is optional, and absent one the bring-your-own-box path is
	// unchanged: the refusal has to read as a choice rather than as a
	// misconfiguration, so it names the alternative.
	if !strings.Contains(stderr, "smith machine setup <login>@<host>") {
		t.Errorf("stderr = %q, want it to name the bring-your-own-box path as the alternative", stderr)
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

// providerBlueprintRequiringAToken is the same adapter naming the environment
// variable its CLI reads the provider token from. The name is all the blueprint
// carries: the value stays in the operator's environment.
const providerBlueprintRequiringAToken = `provider:
  create: [hcloud, server, create, --name, "{{name}}", -o, json]
  requires: [SMITH_TEST_TOKEN]
  extract:
    id: id
    ip: public_net.ipv4.ip
`

func TestMachineCreateRefusesAMissingRequirementBeforeCreatingAnything(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprintRequiringAToken)
	t.Setenv("SMITH_TEST_TOKEN", "unset below")
	if err := os.Unsetenv("SMITH_TEST_TOKEN"); err != nil {
		t.Fatalf("unset SMITH_TEST_TOKEN: %v", err)
	}
	runner := &fakeProviderRunner{}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when a required variable is not set")
	}
	if runner.runs != 0 {
		t.Errorf("ran %d commands, want none before a missing requirement is reported", runner.runs)
	}
	if !strings.Contains(stderr, "SMITH_TEST_TOKEN") || !strings.Contains(stderr, "environment") {
		t.Errorf("stderr = %q, want it to name the variable the provider requires in the environment", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed when the create is refused", stdout)
	}
}

func TestMachineCreateNeverShowsARequiredVariablesValue(t *testing.T) {
	const token = "hcloud-token-a1b2c3d4"
	tests := []struct {
		name   string
		runner *fakeProviderRunner
	}{
		{"a created box", &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}},
		{"a refusing provider", &fakeProviderRunner{stderr: "Error: unauthorized", err: errors.New("exit status 1")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeBlueprint(t, dir, "acme", providerBlueprintRequiringAToken)
			t.Setenv("SMITH_TEST_TOKEN", token)

			stdout, stderr, _ := runCreate(t, dir, tt.runner, "create", "dev", "--blueprint", "acme")

			if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
				t.Errorf("output = %q / %q, want no required variable's value in it", stdout, stderr)
			}
		})
	}
}

func TestMachineCreateSurfacesAFailingProviderCommandsOwnOutput(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprint)
	// The provider's own words, verbatim: smith knows nothing about any
	// provider's error taxonomy and must not try to interpret one.
	const refusal = "Error: POST https://api.example.com/v2/servers: 403 (request \"abc\") requires the tag:create scope"
	runner := &fakeProviderRunner{stderr: refusal + "\n", err: errors.New("exit status 1")}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when the provider command failed")
	}
	if !strings.Contains(stderr, "provider command") || !strings.Contains(stderr, "failed") {
		t.Errorf("stderr = %q, want it to report that the provider command failed", stderr)
	}
	if !strings.Contains(stderr, refusal) {
		t.Errorf("stderr = %q, want the provider's own error text %q shown verbatim", stderr, refusal)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed when the provider refused", stdout)
	}
}

func TestMachineCreateRefusesAnInvalidBlueprintBeforeRunningAnyProviderCommand(t *testing.T) {
	dir := t.TempDir()
	// A valid adapter under a document the schema refuses: validation is free
	// and a botched create costs money, so the refusal comes first.
	writeBlueprint(t, dir, "acme", "terminals: tmux\n"+providerBlueprint)
	runner := &fakeProviderRunner{}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a blueprint with a validation error")
	}
	if runner.runs != 0 {
		t.Errorf("ran %d commands, want none before an invalid blueprint is reported", runner.runs)
	}
	if !strings.Contains(stderr, "terminals") {
		t.Errorf("stderr = %q, want it to report the validation error", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed when the create is refused", stdout)
	}
}

// preferenceAdapter is a complete adapter for one provider, declared
// operator-wide. Every field of it is one a blueprint's own adapter must not
// pick up: a different CLI, a different key reference, and a requirement that
// is deliberately unsatisfiable.
const preferenceAdapter = `provider:
  create: [doctl, compute, droplet, create, "{{name}}", --ssh-keys, "{{ssh_key}}", -o, json]
  list: [doctl, compute, droplet, list, -o, json]
  requires: [SMITH_TEST_PREFERENCE_TOKEN]
  ssh_key: preference-key
  marker:
    arg: "smith:{{value}}"
    read: "tags[*]"
    expect: "smith:{{value}}"
  extract:
    id: id
    ip: networks.v4[type=public].ip_address
`

func TestMachineCreateUsesNoFieldOfThePreferenceAdapter(t *testing.T) {
	dir := t.TempDir()
	writePreferences(t, dir, preferenceAdapter)
	writeBlueprint(t, dir, "acme", providerBlueprint)
	// The preference's requirement is deliberately unsatisfiable, and set-then-
	// unset is how a test unsets a variable and still has it restored.
	t.Setenv("SMITH_TEST_PREFERENCE_TOKEN", "unset below")
	if err := os.Unsetenv("SMITH_TEST_PREFERENCE_TOKEN"); err != nil {
		t.Fatalf("unset SMITH_TEST_PREFERENCE_TOKEN: %v", err)
	}
	runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	stdout, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme")

	// The preference's requires would refuse the create outright, and its
	// ssh_key would be reported as passed: both are fields, and the blueprint's
	// block replaces the preference block whole rather than field by field.
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for the blueprint's own adapter (stderr: %s)", code, stderr)
	}
	want := []string{"server", "create", "--name", "dev", "--type", "cpx31", "-o", "json"}
	if runner.name != "hcloud" || !slices.Equal(runner.args, want) {
		t.Errorf("ran %q %q, want the blueprint's own %q %q", runner.name, runner.args, "hcloud", want)
	}
	if strings.Contains(stdout, "preference-key") {
		t.Errorf("stdout = %q, want no field of the preference adapter used", stdout)
	}
}

// providerBlueprintWithDestroy declares the third verb. smith accepts it as
// data so an adapter written today stays correct when destroy ships, and never
// runs it.
const providerBlueprintWithDestroy = `provider:
  create: [hcloud, server, create, --name, "{{name}}", -o, json]
  list: [hcloud, server, list, -o, json]
  destroy: [hcloud, server, delete, "{{id}}"]
  extract:
    id: id
    ip: public_net.ipv4.ip
`

func TestBlueprintCheckAcceptsADestroyTemplateAsData(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprintWithDestroy)

	stdout, stderr, code := runCheck(t, dir, "check", "acme")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a blueprint declaring a destroy template (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("stdout = %q, want the blueprint reported as valid", stdout)
	}
}

func TestSmithExposesNoCommandThatExecutesDestroy(t *testing.T) {
	// v1 ships no box teardown: the box exists and is billed, and a destroy
	// needs a confirmation poll — a list right after a successful delete still
	// returned the box — plus teardown safety settled first.
	var found []string
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		name := strings.TrimSpace(path + " " + cmd.Name())
		for _, forbidden := range []string{"destroy", "delete"} {
			if cmd.Name() == forbidden {
				found = append(found, name)
			}
		}
		for _, child := range cmd.Commands() {
			walk(child, name)
		}
	}
	walk(newRootCmd(), "")

	if len(found) != 0 {
		t.Errorf("commands %q exist, want no command that executes a provider destroy", found)
	}
}

func TestMachineCreateNeverRunsTheDestroyTemplate(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", providerBlueprintWithDestroy)
	runner := &fakeProviderRunner{stdout: `{"id": 1, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`}

	if _, stderr, code := runCreate(t, dir, runner, "create", "dev", "--blueprint", "acme"); code != 0 {
		t.Fatalf("exit code = %d, want 0 for a created box (stderr: %s)", code, stderr)
	}

	if runner.runs != 1 || !slices.Contains(runner.args, "create") {
		t.Errorf("ran %d commands, last %q %q, want only the create template", runner.runs, runner.name, runner.args)
	}
	if slices.Contains(runner.args, "delete") {
		t.Errorf("ran %q %q, want the destroy template never executed", runner.name, runner.args)
	}
}
