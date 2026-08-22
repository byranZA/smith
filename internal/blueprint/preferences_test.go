package blueprint_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParsePreferencesRefusesACollection(t *testing.T) {
	documents := map[string]string{
		"repos":      "repos:\n  - url: git@github.com:acme/api.git\n",
		"placements": "placements:\n  - to: ~/.npmrc\n",
		"packages":   "packages:\n  - ripgrep\n",
		"tools":      "tools:\n  node: \"20\"\n",
		"env":        "env:\n  TOKEN: op://vault/item/field\n",
	}
	for field, document := range documents {
		t.Run(field, func(t *testing.T) {
			_, err := blueprint.ParsePreferences([]byte(document))
			if err == nil {
				t.Fatalf("ParsePreferences() err = nil, want %q refused as blueprint-only", field)
			}
			if got := err.Error(); !strings.Contains(got, field) {
				t.Errorf("ParsePreferences() error = %q, want it to name %q", got, field)
			}
		})
	}
}

func TestParsePreferencesAcceptsTheFixedKeyFields(t *testing.T) {
	documents := map[string]string{
		"access":    "access: tailscale\n",
		"terminal":  "terminal: tmux\n",
		"workspace": "workspace: /srv/work\n",
		"provider":  "provider:\n  create: [hcloud, server, create]\n",
		"git":       "git:\n  user_name: Ada\n  user_email: ada@example.com\n",
	}
	for field, document := range documents {
		t.Run(field, func(t *testing.T) {
			if _, err := blueprint.ParsePreferences([]byte(document)); err != nil {
				t.Fatalf("ParsePreferences() err = %v, want %q accepted", err, field)
			}
		})
	}
}

func TestParsePreferencesReadsTheFieldsItAccepts(t *testing.T) {
	got, err := blueprint.ParsePreferences([]byte("access: tailscale\nworkspace: /srv/work\ngit:\n  user_name: Ada\n"))
	if err != nil {
		t.Fatalf("ParsePreferences() err = %v, want nil", err)
	}
	if got.Access != "tailscale" || got.Workspace != "/srv/work" || got.Git.UserName != "Ada" {
		t.Errorf("ParsePreferences() = %+v, want access, workspace and the git identity read back", got)
	}
}

func TestParsePreferencesRefusesAnUnrecognisedValue(t *testing.T) {
	_, err := blueprint.ParsePreferences([]byte("access: carrier-pigeon\n"))
	if err == nil {
		t.Fatal("ParsePreferences() err = nil, want an access mode smith does not know refused")
	}
	if got := err.Error(); !strings.Contains(got, "preferences") || !strings.Contains(got, "carrier-pigeon") {
		t.Errorf("ParsePreferences() error = %q, want it to report the preferences as invalid and name the value", got)
	}
}

func TestParsePreferencesReportsAMalformedDocument(t *testing.T) {
	_, err := blueprint.ParsePreferences([]byte("access: [unclosed\n"))
	if err == nil {
		t.Fatal("ParsePreferences() err = nil, want a document that is not YAML reported")
	}
	if got := err.Error(); !strings.Contains(got, "malformed") || !strings.Contains(got, "preferences") {
		t.Errorf("ParsePreferences() error = %q, want it to report the preferences as malformed", got)
	}
}

func TestParsePreferencesAcceptsAnEmptyDocument(t *testing.T) {
	got, err := blueprint.ParsePreferences(nil)
	if err != nil {
		t.Fatalf("ParsePreferences() err = %v, want an empty document accepted", err)
	}
	if got != (blueprint.Preferences{}) {
		t.Errorf("ParsePreferences() = %+v, want every field unset", got)
	}
}
