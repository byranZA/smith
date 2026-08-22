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
		Marker:   blueprint.ProviderMarker{Arg: "smith={{value}}", Read: "labels.smith"},
		Extract:  blueprint.ProviderExtract{ID: "id", IP: "public_net.ipv4.ip"},
	}

	got := provider.Adapt(declared)

	want := provider.Adapter{
		Create:   declared.Create,
		List:     declared.List,
		Destroy:  declared.Destroy,
		Requires: declared.Requires,
		SSHKey:   "my-laptop",
		Marker:   provider.Marker{Arg: "smith={{value}}", Read: "labels.smith"},
		Extract:  provider.Extract{ID: "id", IP: "public_net.ipv4.ip"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Adapt() = %+v, want %+v", got, want)
	}
}
