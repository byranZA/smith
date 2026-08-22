package provider_test

import (
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/provider"
)

func TestAdaptCarriesTheWholeDeclaredBlock(t *testing.T) {
	declared := blueprint.Provider{
		Create:   []string{"doctl", "compute", "droplet", "create", "{{name}}", "-o", "json"},
		List:     []string{"doctl", "compute", "droplet", "list", "-o", "json"},
		Destroy:  []string{"doctl", "compute", "droplet", "delete", "{{id}}"},
		Requires: []string{"DIGITALOCEAN_ACCESS_TOKEN"},
		SSHKey:   "my-laptop",
		Marker:   blueprint.ProviderMarker{Arg: "smith:{{value}}", Read: "tags[*]", Expect: "smith:{{value}}"},
		Record:   blueprint.ProviderRecord{Create: "[*]", List: "[*]"},
		Extract:  blueprint.ProviderExtract{ID: "id", IP: "networks.v4[type=public].ip_address"},
	}

	got := provider.Adapt(declared)

	want := provider.Adapter{
		Create:   declared.Create,
		List:     declared.List,
		Destroy:  declared.Destroy,
		Requires: declared.Requires,
		SSHKey:   "my-laptop",
		Marker:   provider.Marker{Arg: "smith:{{value}}", Read: "tags[*]", Expect: "smith:{{value}}"},
		Record:   provider.Record{Create: "[*]", List: "[*]"},
		Extract:  provider.Extract{ID: "id", IP: "networks.v4[type=public].ip_address"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Adapt() = %+v, want %+v", got, want)
	}
}
