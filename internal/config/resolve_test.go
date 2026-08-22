package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestResolveTakesTheBlueprintOverAPreference(t *testing.T) {
	b := &blueprint.Blueprint{Access: "public"}
	p := &blueprint.Preferences{Access: "tailscale"}

	got := Resolve(Overrides{}, b, p).Access

	if got.Value != "public" || got.Origin != FromBlueprint {
		t.Errorf("resolved access = %q from %q, want %q from %q", got.Value, got.Origin, "public", FromBlueprint)
	}
}

func TestResolveTakesAPreferenceOverTheBuiltInDefault(t *testing.T) {
	b := &blueprint.Blueprint{}
	p := &blueprint.Preferences{Access: "tailscale"}

	got := Resolve(Overrides{}, b, p).Access

	if got.Value != "tailscale" || got.Origin != FromPreferences {
		t.Errorf("resolved access = %q from %q, want %q from %q", got.Value, got.Origin, "tailscale", FromPreferences)
	}
}

func TestResolveFallsThroughToTheBuiltInDefaults(t *testing.T) {
	got := Resolve(Overrides{}, nil, nil)

	for _, tt := range []struct {
		field string
		got   Value
		want  string
	}{
		{"access", got.Access, "public"},
		{"terminal", got.Terminal, "tmux"},
		{"workspace", got.Workspace, "~/workspace"},
	} {
		if tt.got.Value != tt.want || tt.got.Origin != FromDefault {
			t.Errorf("resolved %s = %q from %q, want %q from %q", tt.field, tt.got.Value, tt.got.Origin, tt.want, FromDefault)
		}
	}
}

func TestResolveTakesTheFlagOverEverything(t *testing.T) {
	b := &blueprint.Blueprint{Access: "public"}
	p := &blueprint.Preferences{Access: "public"}

	got := Resolve(Overrides{Access: "tailscale"}, b, p).Access

	if got.Value != "tailscale" || got.Origin != FromFlag {
		t.Errorf("resolved access = %q from %q, want %q from %q", got.Value, got.Origin, "tailscale", FromFlag)
	}
}

func TestResolveResolvesEachFieldOnItsOwn(t *testing.T) {
	b := &blueprint.Blueprint{Access: "public"}
	p := &blueprint.Preferences{Access: "tailscale", Workspace: "~/dev"}

	got := Resolve(Overrides{}, b, p)

	if got.Access.Value != "public" {
		t.Errorf("resolved access = %q, want the blueprint's %q", got.Access.Value, "public")
	}
	if got.Workspace.Value != "~/dev" || got.Workspace.Origin != FromPreferences {
		t.Errorf("resolved workspace = %q from %q, want the untouched preference %q", got.Workspace.Value, got.Workspace.Origin, "~/dev")
	}
}

func TestResolveAppliesPreferencesWithNoBlueprintAtAll(t *testing.T) {
	p := &blueprint.Preferences{Access: "tailscale"}

	got := Resolve(Overrides{}, nil, p).Access

	if got.Value != "tailscale" || got.Origin != FromPreferences {
		t.Errorf("resolved access = %q from %q, want %q from %q", got.Value, got.Origin, "tailscale", FromPreferences)
	}
}

func TestEffectiveReportsEveryFieldWithItsOrigin(t *testing.T) {
	got := Resolve(Overrides{Access: "tailscale"}, &blueprint.Blueprint{Terminal: "tmux"}, nil).String()

	for _, want := range []string{"access", "tailscale", string(FromFlag), "terminal", string(FromBlueprint), "workspace", "~/workspace", string(FromDefault)} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved configuration = %q, want it to contain %q", got, want)
		}
	}
}

func TestResolveReplacesThePreferenceProviderWholesale(t *testing.T) {
	b := &blueprint.Blueprint{Provider: &blueprint.Provider{Create: []string{"doctl", "compute", "droplet", "create"}}}
	p := &blueprint.Preferences{Provider: &blueprint.Provider{
		Create:   []string{"hcloud", "server", "create"},
		List:     []string{"hcloud", "server", "list"},
		Destroy:  []string{"hcloud", "server", "delete"},
		Requires: []string{"HCLOUD_TOKEN"},
		SSHKey:   "byran@laptop",
		Marker:   blueprint.ProviderMarker{Arg: "smith", Read: ".labels.smith"},
		Extract:  blueprint.ProviderExtract{ID: ".id", IP: ".public_net.ipv4.ip"},
	}}

	got := Resolve(Overrides{}, b, p).Provider

	if got.Provider != b.Provider {
		t.Fatalf("resolved provider = %+v, want the blueprint's adapter %+v", got.Provider, b.Provider)
	}
	for _, tt := range []struct {
		field string
		got   any
	}{
		{"list", got.Provider.List},
		{"destroy", got.Provider.Destroy},
		{"requires", got.Provider.Requires},
		{"ssh_key", got.Provider.SSHKey},
		{"marker", got.Provider.Marker},
		{"extract", got.Provider.Extract},
	} {
		if !reflect.ValueOf(tt.got).IsZero() {
			t.Errorf("resolved provider %s = %+v, want nothing inherited from the foreign preference adapter", tt.field, tt.got)
		}
	}
	if got.Origin != FromBlueprintReplacing {
		t.Errorf("resolved provider origin = %q, want %q", got.Origin, FromBlueprintReplacing)
	}
}

func TestResolveInheritsThePreferenceProviderEntire(t *testing.T) {
	adapter := &blueprint.Provider{
		Create:   []string{"hcloud", "server", "create"},
		Requires: []string{"HCLOUD_TOKEN"},
		SSHKey:   "byran@laptop",
	}
	p := &blueprint.Preferences{Provider: adapter}

	got := Resolve(Overrides{}, &blueprint.Blueprint{Access: "public"}, p).Provider

	if got.Provider != adapter || got.Origin != FromPreferences {
		t.Errorf("resolved provider = %+v from %q, want the preference adapter from %q", got.Provider, got.Origin, FromPreferences)
	}
}

func TestResolveLeavesTheProviderAbsentWhenNobodyDeclaresOne(t *testing.T) {
	got := Resolve(Overrides{}, nil, nil).Provider

	if got.Provider != nil {
		t.Errorf("resolved provider = %+v, want none", got.Provider)
	}
}

func TestResolveKeepsEveryOtherFieldFieldLevelWhenTheProviderIsReplaced(t *testing.T) {
	b := &blueprint.Blueprint{
		Access:   "public",
		Provider: &blueprint.Provider{Create: []string{"doctl", "compute", "droplet", "create"}},
	}
	p := &blueprint.Preferences{
		Access:    "tailscale",
		Workspace: "~/dev",
		Terminal:  "tmux",
		Provider:  &blueprint.Provider{Create: []string{"hcloud", "server", "create"}},
	}

	got := Resolve(Overrides{}, b, p)

	if got.Workspace.Value != "~/dev" || got.Workspace.Origin != FromPreferences {
		t.Errorf("resolved workspace = %q from %q, want the untouched preference %q", got.Workspace.Value, got.Workspace.Origin, "~/dev")
	}
	if got.Terminal.Value != "tmux" || got.Terminal.Origin != FromPreferences {
		t.Errorf("resolved terminal = %q from %q, want the untouched preference %q", got.Terminal.Value, got.Terminal.Origin, "tmux")
	}
	if got.Access.Value != "public" || got.Access.Origin != FromBlueprint {
		t.Errorf("resolved access = %q from %q, want the blueprint's %q", got.Access.Value, got.Access.Origin, "public")
	}
}

func TestEffectiveReportsWhichAdapterIsInPlay(t *testing.T) {
	replaced := Resolve(Overrides{},
		&blueprint.Blueprint{Provider: &blueprint.Provider{Create: []string{"doctl", "compute", "droplet", "create"}}},
		&blueprint.Preferences{Provider: &blueprint.Provider{Create: []string{"hcloud", "server", "create"}}},
	).String()

	for _, want := range []string{"provider", "doctl", string(FromBlueprintReplacing)} {
		if !strings.Contains(replaced, want) {
			t.Errorf("resolved configuration = %q, want it to contain %q", replaced, want)
		}
	}
	if strings.Contains(replaced, "hcloud") {
		t.Errorf("resolved configuration = %q, want no trace of the replaced preference adapter", replaced)
	}

	inherited := Resolve(Overrides{}, &blueprint.Blueprint{},
		&blueprint.Preferences{Provider: &blueprint.Provider{Create: []string{"hcloud", "server", "create"}}},
	).String()

	for _, want := range []string{"provider", "hcloud", string(FromPreferences)} {
		if !strings.Contains(inherited, want) {
			t.Errorf("resolved configuration = %q, want it to contain %q", inherited, want)
		}
	}
}
