package config

import (
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
