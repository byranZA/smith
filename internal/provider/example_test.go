package provider_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/provider"
)

// exampleAdapters is the directory of shipped worked-example adapters, read
// here through the real parser so a documented adapter that no longer drives
// smith fails in the test suite rather than in an operator's terminal.
const exampleAdapters = "../../docs/examples/adapters"

// exampleAdapter reads one shipped example adapter the way smith reads any
// provider block: parsed as a document, then adapted into what smith runs.
func exampleAdapter(t *testing.T, file string) provider.Adapter {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(exampleAdapters, file))
	if err != nil {
		t.Fatalf("read example adapter: %v", err)
	}
	doc, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("Parse(%s) err = %v, want nil", file, err)
	}
	if doc.Provider == nil {
		t.Fatalf("example adapter %s declares no provider block", file)
	}
	return provider.Adapt(*doc.Provider)
}

// response reads a recorded provider response from testdata. The two
// DigitalOcean files are what a real lifecycle run answered with — one under a
// token that could create tags, one under a token that could not.
func response(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatalf("read response fixture: %v", err)
	}
	return string(data)
}

// tokenIsPresent satisfies the adapters' declared credential requirement. The
// value is never read by anything — smith checks the name is present and hands
// the environment to the command — so a placeholder is the honest fixture.
func tokenIsPresent(t *testing.T) {
	t.Helper()
	t.Setenv("DIGITALOCEAN_ACCESS_TOKEN", "not-a-real-token")
}

// The two DigitalOcean adapters and the create response each one was recorded
// against. They are the regression fixture for "an adapter is data, not code":
// one stamps a native tag, the other lets the box's own name be the marker, and
// smith runs both down the same path with no branch between them. A change that
// makes the name-convention adapter need special handling has leaked provider
// knowledge into smith, and these tests are what catches it.
var digitalOcean = []struct {
	name     string
	file     string
	response string
}{
	{"tag marker", "doctl.yaml", "doctl-create-tagged.json"},
	{"name marker", "doctl-name-marker.yaml", "doctl-create-named.json"},
}

func TestBothDigitalOceanAdaptersProduceTheSameCanonicalRecord(t *testing.T) {
	tokenIsPresent(t)
	want := provider.Box{ID: "593069736", IP: "203.0.113.10"}

	for _, tt := range digitalOcean {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: response(t, tt.response)}

			got, err := provider.Create(context.Background(), runner, &fakeClock{}, exampleAdapter(t, tt.file), "dev")

			if err != nil {
				t.Fatalf("Create() err = %v, want nil", err)
			}
			if got != want {
				t.Errorf("Create() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestTheDigitalOceanAdaptersDifferOnlyInWhatTheyStamp(t *testing.T) {
	tokenIsPresent(t)
	argv := map[string][]string{}
	for _, tt := range digitalOcean {
		runner := &fakeRunner{stdout: response(t, tt.response)}
		if _, err := provider.Create(context.Background(), runner, &fakeClock{}, exampleAdapter(t, tt.file), "dev"); err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		argv[tt.name] = append([]string{runner.name}, runner.args...)
	}

	tagged, named := argv["tag marker"], argv["name marker"]
	if !slices.Contains(tagged, "smith:dev") {
		t.Errorf("tag-marker command = %q, want it to stamp %q", tagged, "smith:dev")
	}
	if slices.Contains(named, "--tag-name") {
		t.Errorf("name-marker command = %q, want no tag argument left behind", named)
	}
	// The name-marker adapter declares an empty marker argument and nothing
	// else, so what it runs is the other command with the tag flag and its
	// value dropped. Anything more is a branch smith should not have.
	stripped := slices.DeleteFunc(slices.Clone(tagged), func(arg string) bool {
		return arg == "--tag-name" || arg == "smith:dev"
	})
	if !slices.Equal(stripped, named) {
		t.Errorf("name-marker command = %q, want the tag-marker command without its tag argument: %q", named, stripped)
	}
}

func TestEveryShippedExampleAdapterIsOneSmithCanRun(t *testing.T) {
	entries, err := os.ReadDir(exampleAdapters)
	if err != nil {
		t.Fatalf("read example adapters: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no example adapters are shipped, want at least the worked doctl example")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			if err := provider.Validate(exampleAdapter(t, entry.Name())); err != nil {
				t.Errorf("Validate(%s) err = %v, want nil", entry.Name(), err)
			}
		})
	}
}
