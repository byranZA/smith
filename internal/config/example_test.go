package config_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// exampleHome is the shipped example config home, read through the real loader so the docs cannot drift from the schema.
const exampleHome = "../../docs/examples"

func TestTheShippedExampleBlueprintValidates(t *testing.T) {
	t.Parallel()
	if _, _, err := config.Load(config.NewHome(exampleHome), "acme"); err != nil {
		t.Fatalf("Load(example acme) err = %v, want nil", err)
	}
}

func TestTheShippedExampleBlueprintCoversTheWholeSurface(t *testing.T) {
	t.Parallel()
	got, _, err := config.Load(config.NewHome(exampleHome), "acme")
	if err != nil {
		t.Fatalf("Load(example acme) err = %v, want nil", err)
	}

	switch {
	case len(got.Repos) < 2:
		t.Errorf("example declares %d repos, want at least two", len(got.Repos))
	case len(got.Repos[0].Tools) == 0:
		t.Errorf("example repo tools = %v, want a per-repo tools override", got.Repos[0].Tools)
	case len(got.Placements) == 0:
		t.Errorf("example placements = %v, want a box placement", got.Placements)
	case len(got.Repos[0].Placements) == 0:
		t.Errorf("example repo placements = %v, want a repo placement", got.Repos[0].Placements)
	case got.Git.UserName == "" || got.Git.UserEmail == "":
		t.Errorf("example Git = %+v, want a git identity", got.Git)
	case got.Provider == nil:
		t.Error("example provider = nil, want a provider block")
	}
}

func TestTheShippedExampleBlueprintShowsEveryReferenceScheme(t *testing.T) {
	t.Parallel()
	got, _, err := config.Load(config.NewHome(exampleHome), "acme")
	if err != nil {
		t.Fatalf("Load(example acme) err = %v, want nil", err)
	}

	for _, scheme := range []string{"env:", "file:", "literal:"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			if !referencesScheme(got.Env, scheme) {
				t.Errorf("example env = %v, want a %q reference", got.Env, scheme)
			}
		})
	}
}

// referencesScheme reports whether any value in env carries the given scheme.
func referencesScheme(env map[string]string, scheme string) bool {
	for _, value := range env {
		if strings.HasPrefix(value, scheme) {
			return true
		}
	}
	return false
}

func TestTheShippedExamplePreferencesValidate(t *testing.T) {
	t.Parallel()
	got, err := config.LoadPreferences(config.NewHome(exampleHome))
	if err != nil {
		t.Fatalf("LoadPreferences(example) err = %v, want nil", err)
	}
	declared := got.Declared
	if declared.Access == "" || declared.Terminal == "" || declared.Workspace == "" || declared.Provider == nil {
		t.Errorf("example preferences = %+v, want access, terminal, workspace and a provider", declared)
	}
	if declared.Git.UserName == "" || declared.Git.UserEmail == "" {
		t.Errorf("example preferences Git = %+v, want a git identity", declared.Git)
	}
}
