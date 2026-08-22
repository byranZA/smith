package config_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

// exampleHome is the shipped example config home, laid out exactly as an
// operator's ~/.smith/ is so the examples are copied rather than translated.
// Reading it through the real loader is what keeps the documentation from
// drifting out of the schema: an example that no longer validates fails here
// before an operator ever pastes it.
const exampleHome = "../../docs/examples"

func TestTheShippedExampleBlueprintValidates(t *testing.T) {
	got, err := config.Load(config.NewHome(exampleHome), "acme")
	if err != nil {
		t.Fatalf("Load(example acme) err = %v, want nil", err)
	}

	// The example earns its place by covering the whole surface, so a field
	// quietly dropped from it is a failure too.
	switch {
	case len(got.Repos) < 2:
		t.Errorf("example declares %d repos, want at least two", len(got.Repos))
	case len(got.Repos[0].Tools) == 0:
		t.Error("example declares no per-repo tools override")
	case len(got.Placements) == 0:
		t.Error("example declares no box placement")
	case len(got.Repos[0].Placements) == 0:
		t.Error("example declares no repo placement")
	case got.Git.UserName == "" || got.Git.UserEmail == "":
		t.Errorf("example Git = %+v, want a git identity", got.Git)
	case got.Provider == nil:
		t.Error("example declares no provider block")
	}

	// All three reference schemes, so the example shows what each is for.
	for _, scheme := range []string{"env:", "file:", "literal:"} {
		if !referencesScheme(got.Env, scheme) {
			t.Errorf("example env declares no %q reference", scheme)
		}
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
	got, err := config.LoadPreferences(config.NewHome(exampleHome))
	if err != nil {
		t.Fatalf("LoadPreferences(example) err = %v, want nil", err)
	}
	if got.Access == "" || got.Terminal == "" || got.Workspace == "" || got.Provider == nil {
		t.Errorf("example preferences = %+v, want access, terminal, workspace and a provider", got)
	}
	if got.Git.UserName == "" || got.Git.UserEmail == "" {
		t.Errorf("example preferences Git = %+v, want a git identity", got.Git)
	}
}
