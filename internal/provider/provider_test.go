package provider_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/provider"
)

// fakeRunner stands in for the provider CLI at the process boundary — the one
// legitimate fake here, since running doctl or hcloud is a real system edge. It
// records what it was asked to run and replays a canned response.
type fakeRunner struct {
	stdout string
	stderr string
	err    error

	name string
	args []string
	runs int
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.name, f.args, f.runs = name, slices.Clone(args), f.runs+1
	if _, err := io.WriteString(stdout, f.stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, f.stderr); err != nil {
		return err
	}
	return f.err
}

// hetzner is an adapter shaped like the operator would write one: templates the
// operator owns, with only smith's own placeholder in them.
func hetzner() provider.Adapter {
	return provider.Adapter{
		Create:  []string{"hcloud", "server", "create", "--name", "{{name}}", "--type", "cpx31", "-o", "json"},
		Extract: provider.Extract{ID: "id", IP: "public_net.ipv4.ip"},
	}
}

const hetznerResponse = `{"id": 519823476123, "public_net": {"ipv4": {"ip": "203.0.113.10"}}}`

func TestCreateRunsTheTemplateWithTheBoxNameSubstituted(t *testing.T) {
	runner := &fakeRunner{stdout: hetznerResponse}

	if _, err := provider.Create(context.Background(), runner, hetzner(), "dev"); err != nil {
		t.Fatalf("Create() err = %v", err)
	}

	if runner.name != "hcloud" {
		t.Errorf("command = %q, want %q", runner.name, "hcloud")
	}
	want := []string{"server", "create", "--name", "dev", "--type", "cpx31", "-o", "json"}
	if !slices.Equal(runner.args, want) {
		t.Errorf("args = %q, want %q", runner.args, want)
	}
}

func TestCreateExtractsTheBoxByDottedPath(t *testing.T) {
	runner := &fakeRunner{stdout: hetznerResponse}

	box, err := provider.Create(context.Background(), runner, hetzner(), "dev")
	if err != nil {
		t.Fatalf("Create() err = %v", err)
	}

	want := provider.Box{ID: "519823476123", IP: "203.0.113.10"}
	if box != want {
		t.Errorf("Create() = %+v, want %+v", box, want)
	}
}

func TestCreateSurfacesTheProvidersOwnError(t *testing.T) {
	runner := &fakeRunner{stderr: "Error: missing the required permission ssh_key:read", err: errors.New("exit status 1")}

	_, err := provider.Create(context.Background(), runner, hetzner(), "dev")

	if err == nil {
		t.Fatal("Create() err = nil, want the failing provider command reported")
	}
	if !strings.Contains(err.Error(), "ssh_key:read") {
		t.Errorf("Create() err = %v, want the provider's own error text shown", err)
	}
}

func TestCreateReportsOutputThatIsNotJSON(t *testing.T) {
	runner := &fakeRunner{stdout: "Server dev created\n"}

	_, err := provider.Create(context.Background(), runner, hetzner(), "dev")

	if err == nil {
		t.Fatal("Create() err = nil, want output that is not JSON reported")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("Create() err = %v, want it to say the output could not be read as JSON", err)
	}
}

func TestCreateReportsAnExtractorPathThatMatchedNothing(t *testing.T) {
	adapter := hetzner()
	adapter.Extract.ID = "droplet.id"
	runner := &fakeRunner{stdout: hetznerResponse}

	_, err := provider.Create(context.Background(), runner, adapter, "dev")

	if err == nil {
		t.Fatal("Create() err = nil, want a path matching nothing reported")
	}
	if !strings.Contains(err.Error(), "droplet.id") || !strings.Contains(err.Error(), "id path") {
		t.Errorf("Create() err = %v, want it to name the adapter's id path", err)
	}
}

func TestCreateRefusesAnAdapterWithNoCreateTemplate(t *testing.T) {
	runner := &fakeRunner{}

	if _, err := provider.Create(context.Background(), runner, provider.Adapter{}, "dev"); err == nil {
		t.Fatal("Create() err = nil, want an adapter with no create template refused")
	}
	if runner.runs != 0 {
		t.Errorf("ran %d commands, want none", runner.runs)
	}
}

func TestAdaptCarriesTheWholeProviderBlock(t *testing.T) {
	declared := blueprint.Provider{
		Create:   []string{"hcloud", "server", "create"},
		List:     []string{"hcloud", "server", "list"},
		Destroy:  []string{"hcloud", "server", "delete", "{{id}}"},
		Requires: []string{"HCLOUD_TOKEN"},
		SSHKey:   "my-laptop",
		Marker:   blueprint.ProviderMarker{Arg: "smith={{value}}", Read: "labels.smith", Expect: "{{value}}"},
		Extract:  blueprint.ProviderExtract{ID: "id", IP: "public_net.ipv4.ip"},
	}

	got := provider.Adapt(declared)

	want := provider.Adapter{
		Create:   declared.Create,
		List:     declared.List,
		Destroy:  declared.Destroy,
		Requires: declared.Requires,
		SSHKey:   "my-laptop",
		Marker:   provider.Marker{Arg: "smith={{value}}", Read: "labels.smith", Expect: "{{value}}"},
		Extract:  provider.Extract{ID: "id", IP: "public_net.ipv4.ip"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Adapt() = %+v, want %+v", got, want)
	}
}

// doctlResponse is a real doctl create response, trimmed: the record arrives
// wrapped in an array, and the address list puts the private entry second.
const doctlResponse = `[{"id": 593069736, "tags": null,
  "networks": {"v4": [
    {"ip_address": "203.0.113.10", "type": "public"},
    {"ip_address": "10.135.0.4", "type": "private"}]}}]`

func TestCreateReadsTheRecordOutOfWhateverEnvelopeTheProviderUses(t *testing.T) {
	tests := []struct {
		name     string
		response string
		adapter  provider.Adapter
		want     provider.Box
	}{
		{
			name:     "an array response",
			response: doctlResponse,
			adapter: provider.Adapter{
				Create:  []string{"doctl", "compute", "droplet", "create", "{{name}}", "-o", "json"},
				Record:  provider.Record{Create: "[*]"},
				Extract: provider.Extract{ID: "id", IP: "networks.v4[type=public].ip_address"},
			},
			want: provider.Box{ID: "593069736", IP: "203.0.113.10"},
		},
		{
			name:     "an object envelope",
			response: `{"server": {"id": 55512345, "public_net": {"ipv4": {"ip": "5.75.10.20"}}}}`,
			adapter: provider.Adapter{
				Create:  []string{"hcloud", "server", "create", "--name", "{{name}}", "-o", "json"},
				Record:  provider.Record{Create: "server"},
				Extract: provider.Extract{ID: "id", IP: "public_net.ipv4.ip"},
			},
			want: provider.Box{ID: "55512345", IP: "5.75.10.20"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: tt.response}

			box, err := provider.Create(context.Background(), runner, tt.adapter, "dev")
			if err != nil {
				t.Fatalf("Create() err = %v", err)
			}
			if box != tt.want {
				t.Errorf("Create() = %+v, want %+v", box, tt.want)
			}
		})
	}
}

func TestCreateReportsARecordPathThatMatchedNothing(t *testing.T) {
	adapter := hetzner()
	adapter.Record.Create = "droplet"
	runner := &fakeRunner{stdout: hetznerResponse}

	_, err := provider.Create(context.Background(), runner, adapter, "dev")

	if err == nil {
		t.Fatal("Create() err = nil, want a record path matching nothing reported")
	}
	if !strings.Contains(err.Error(), "droplet") || !strings.Contains(err.Error(), "record path") {
		t.Errorf("Create() err = %v, want it to name the adapter's record path", err)
	}
}

// optional is an adapter whose create template passes both optional values
// behind flags, which is the shape the drop rule exists for.
func optional() provider.Adapter {
	a := hetzner()
	a.Create = []string{"hcloud", "server", "create", "--name", "{{name}}",
		"--ssh-key", "{{ssh_key}}", "--label", "{{marker_arg}}", "-o", "json"}
	return a
}

func TestCreateRendersTheOptionalArguments(t *testing.T) {
	adapter := optional()
	adapter.SSHKey = "my-laptop"
	adapter.Marker.Arg = "smith"
	runner := &fakeRunner{stdout: hetznerResponse}

	if _, err := provider.Create(context.Background(), runner, adapter, "dev"); err != nil {
		t.Fatalf("Create() err = %v", err)
	}

	want := []string{"server", "create", "--name", "dev",
		"--ssh-key", "my-laptop", "--label", "smith", "-o", "json"}
	if !slices.Equal(runner.args, want) {
		t.Errorf("args = %q, want %q", runner.args, want)
	}
}

func TestCreateDropsAnArgumentThatRendersEmptyAndTheFlagBeforeIt(t *testing.T) {
	tests := []struct {
		name    string
		adapter provider.Adapter
		want    []string
	}{
		{
			name:    "no ssh key drops --ssh-key",
			adapter: func() provider.Adapter { a := optional(); a.Marker.Arg = "smith"; return a }(),
			want:    []string{"server", "create", "--name", "dev", "--label", "smith", "-o", "json"},
		},
		{
			name:    "no marker argument drops --label",
			adapter: func() provider.Adapter { a := optional(); a.SSHKey = "my-laptop"; return a }(),
			want:    []string{"server", "create", "--name", "dev", "--ssh-key", "my-laptop", "-o", "json"},
		},
		{
			name:    "neither declared drops both",
			adapter: optional(),
			want:    []string{"server", "create", "--name", "dev", "-o", "json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: hetznerResponse}

			if _, err := provider.Create(context.Background(), runner, tt.adapter, "dev"); err != nil {
				t.Fatalf("Create() err = %v", err)
			}

			if !slices.Equal(runner.args, tt.want) {
				t.Errorf("args = %q, want %q", runner.args, tt.want)
			}
			if slices.Contains(runner.args, "") {
				t.Errorf("args = %q, want no empty argument left behind", runner.args)
			}
		})
	}
}

func TestCreateKeepsANonFlagBeforeADroppedArgument(t *testing.T) {
	adapter := hetzner()
	adapter.Create = []string{"doctl", "compute", "droplet", "create", "{{name}}", "--ssh-keys", "{{ssh_key}}", "-o", "json"}
	runner := &fakeRunner{stdout: hetznerResponse}

	if _, err := provider.Create(context.Background(), runner, adapter, "dev"); err != nil {
		t.Fatalf("Create() err = %v", err)
	}

	want := []string{"compute", "droplet", "create", "dev", "-o", "json"}
	if !slices.Equal(runner.args, want) {
		t.Errorf("args = %q, want %q", runner.args, want)
	}
}

func TestCreateRefusesATemplateWithAnUnknownPlaceholder(t *testing.T) {
	adapter := hetzner()
	adapter.Create = append(adapter.Create, "--ssh-key", "{{sshkey}}")
	runner := &fakeRunner{}

	_, err := provider.Create(context.Background(), runner, adapter, "dev")

	if err == nil {
		t.Fatal("Create() err = nil, want an unknown placeholder refused")
	}
	for _, want := range []string{"{{sshkey}}", "{{name}}", "{{marker_arg}}", "{{ssh_key}}", "{{id}}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Create() err = %v, want it to name %s", err, want)
		}
	}
	if runner.runs != 0 {
		t.Errorf("ran %d commands, want none", runner.runs)
	}
}

func TestValidateReportsAnUnknownPlaceholderInAnyTemplate(t *testing.T) {
	tests := []struct {
		name    string
		adapter provider.Adapter
	}{
		{"in create", provider.Adapter{Create: []string{"hcloud", "server", "create", "{{box}}"}}},
		{"in list", provider.Adapter{Create: []string{"hcloud"}, List: []string{"hcloud", "server", "list", "{{box}}"}}},
		{"in destroy", provider.Adapter{Create: []string{"hcloud"}, Destroy: []string{"hcloud", "server", "delete", "{{box}}"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.Validate(tt.adapter)

			if err == nil {
				t.Fatal("Validate() err = nil, want an unknown placeholder reported")
			}
			if !strings.Contains(err.Error(), "{{box}}") {
				t.Errorf("Validate() err = %v, want it to name {{box}}", err)
			}
		})
	}
}

func TestValidateAcceptsTheFourPlaceholdersSmithSupplies(t *testing.T) {
	adapter := provider.Adapter{
		Create:  []string{"hcloud", "server", "create", "{{name}}", "{{marker_arg}}", "{{ssh_key}}"},
		List:    []string{"hcloud", "server", "list"},
		Destroy: []string{"hcloud", "server", "delete", "{{id}}"},
	}

	if err := provider.Validate(adapter); err != nil {
		t.Errorf("Validate() err = %v, want the placeholders smith supplies accepted", err)
	}
}

// The three marker shapes the adapter contract has to carry with no branch in
// smith's code: hcloud's key/value label, doctl's opaque tag, and — for a token
// without tag permission — the box's own name, which passes no create argument
// at all.
func TestCreateStampsTheBoxWithTheMarkerArgumentRenderedFromTheBoxName(t *testing.T) {
	tests := []struct {
		name   string
		marker provider.Marker
		want   []string
	}{
		{
			name:   "an hcloud label",
			marker: provider.Marker{Arg: "smith={{value}}", Read: "labels.smith", Expect: "{{value}}"},
			want:   []string{"server", "create", "--name", "dev", "--label", "smith=dev", "-o", "json"},
		},
		{
			name:   "a doctl tag",
			marker: provider.Marker{Arg: "smith:{{value}}", Read: "tags[*]", Expect: "smith:{{value}}"},
			want:   []string{"server", "create", "--name", "dev", "--label", "smith:dev", "-o", "json"},
		},
		{
			name:   "the box's own name, stamped by no argument at all",
			marker: provider.Marker{Read: "name", Expect: "{{value}}"},
			want:   []string{"server", "create", "--name", "dev", "-o", "json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := optional()
			adapter.SSHKey = "my-laptop"
			adapter.Marker = tt.marker
			runner := &fakeRunner{stdout: hetznerResponse}

			if _, err := provider.Create(context.Background(), runner, adapter, "dev"); err != nil {
				t.Fatalf("Create() err = %v", err)
			}

			want := slices.Insert(slices.Clone(tt.want), 4, "--ssh-key", "my-laptop")
			if !slices.Equal(runner.args, want) {
				t.Errorf("args = %q, want %q", runner.args, want)
			}
		})
	}
}

func TestValidateRefusesAMarkerFieldWithAnUnknownPlaceholder(t *testing.T) {
	tests := []struct {
		name   string
		marker provider.Marker
	}{
		{"in arg", provider.Marker{Arg: "smith={{box}}"}},
		{"in expect", provider.Marker{Arg: "smith={{value}}", Expect: "{{box}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := hetzner()
			adapter.Marker = tt.marker

			err := provider.Validate(adapter)

			if err == nil {
				t.Fatal("Validate() err = nil, want an unknown placeholder reported")
			}
			for _, want := range []string{"{{box}}", "{{value}}"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Validate() err = %v, want it to name %s", err, want)
				}
			}
		})
	}
}

func TestValidateAcceptsTheBoxNamePlaceholderInTheMarker(t *testing.T) {
	adapter := hetzner()
	adapter.Marker = provider.Marker{Arg: "smith:{{value}}", Read: "tags[*]", Expect: "smith:{{value}}"}

	if err := provider.Validate(adapter); err != nil {
		t.Errorf("Validate() err = %v, want {{value}} accepted in the marker", err)
	}
}
