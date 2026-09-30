package starter_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/starter"
)

func TestScaffoldCreatesBothStartersInAnEmptyHome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".smith")

	got, err := starter.Scaffold(config.NewHome(dir), blueprint.Git{})
	if err != nil {
		t.Fatalf("Scaffold() err = %v, want nil", err)
	}

	want := []starter.Outcome{
		{Path: filepath.Join(dir, "preferences.yaml"), Created: true},
		{Path: filepath.Join(dir, "blueprints", "personal.yaml"), Created: true},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Scaffold() = %+v, want %+v", got, want)
	}
}

// scaffold runs Scaffold against a fresh config home and fails the test if it
// errors, returning its outcomes.
func scaffold(t *testing.T, dir string) []starter.Outcome {
	t.Helper()
	got, err := starter.Scaffold(config.NewHome(dir), blueprint.Git{})
	if err != nil {
		t.Fatalf("Scaffold() err = %v, want nil", err)
	}
	return got
}

// writeFile writes content to path under dir, creating its parents.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readFile returns the content of path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestTheStarterBlueprintLoadsToBuiltInDefaults(t *testing.T) {
	dir := t.TempDir()
	scaffold(t, dir)

	got, _, err := config.Load(config.NewHome(dir), starter.Blueprint)
	if err != nil {
		t.Fatalf("Load(starter blueprint) err = %v, want nil", err)
	}
	resolved := config.Resolve(config.Overrides{}, &got, &blueprint.Preferences{})
	for name, v := range map[string]config.Value{"access": resolved.Access, "terminal": resolved.Terminal, "workspace": resolved.Workspace} {
		if v.Origin != config.FromDefault {
			t.Errorf("%s = %v, want the built-in default", name, v)
		}
	}
}

func TestTheStarterPreferencesLoad(t *testing.T) {
	dir := t.TempDir()
	scaffold(t, dir)

	got, err := config.LoadPreferences(config.NewHome(dir))
	if err != nil {
		t.Fatalf("LoadPreferences(starter) err = %v, want nil", err)
	}
	if !got.Found {
		t.Error("LoadPreferences(starter) Found = false, want the starter read")
	}
}

func TestScaffoldLeavesAnExistingFileAlone(t *testing.T) {
	for _, existing := range []string{"preferences.yaml", filepath.Join("blueprints", "personal.yaml")} {
		t.Run(existing, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, existing)
			writeFile(t, path, "operator: content\n")

			got := scaffold(t, dir)

			if content := readFile(t, path); content != "operator: content\n" {
				t.Errorf("%s = %q, want it unchanged", existing, content)
			}
			for _, o := range got {
				if want := o.Path != path; o.Created != want {
					t.Errorf("outcome %+v, want Created = %v", o, want)
				}
			}
		})
	}
}

func TestScaffoldingTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	scaffold(t, dir)
	before := snapshot(t, dir)

	got := scaffold(t, dir)

	for _, o := range got {
		if o.Created {
			t.Errorf("second run outcome %+v, want every file left alone", o)
		}
	}
	if after := snapshot(t, dir); !maps.Equal(before, after) {
		t.Errorf("config home after second run = %v, want %v", after, before)
	}
}

// snapshot maps every file under dir to its content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[path] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

func TestScaffoldLeavesOtherFilesInTheHomeAlone(t *testing.T) {
	dir := t.TempDir()
	home := config.NewHome(dir)
	acme := filepath.Join(dir, "blueprints", "acme.yaml")
	writeFile(t, acme, "access: tailscale\n")
	writeFile(t, home.InventoryPath(), `{"boxes":[]}`)
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.swp\ncache/\n")

	got := scaffold(t, dir)

	if !got[1].Created {
		t.Errorf("starter blueprint outcome %+v, want it created beside acme", got[1])
	}
	for path, want := range map[string]string{
		acme:                             "access: tailscale\n",
		home.InventoryPath():             `{"boxes":[]}`,
		filepath.Join(dir, ".gitignore"): "*.swp\ncache/\n",
	} {
		if content := readFile(t, path); content != want {
			t.Errorf("%s = %q, want %q", path, content, want)
		}
	}
}

func TestScaffoldNamesThePathItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	blueprints := filepath.Join(dir, "blueprints")
	// A file where the blueprints directory belongs cannot be written into,
	// whatever the account running the test is allowed to do.
	writeFile(t, blueprints, "not a directory\n")

	_, err := starter.Scaffold(config.NewHome(dir), blueprint.Git{})

	if err == nil || !strings.Contains(err.Error(), blueprints) {
		t.Errorf("Scaffold() err = %v, want an error naming %s", err, blueprints)
	}
}

// verifiedAdapter is the worked DigitalOcean adapter in the docs, verified
// against a real lifecycle run. The starter preferences carry it commented out.
const verifiedAdapter = "../../docs/examples/adapters/doctl.yaml"

// fixedKeys are the top-level keys a preferences file and a blueprint share;
// blueprintOnlyKeys are the ones only a blueprint declares.
var (
	fixedKeys         = []string{"access", "terminal", "workspace", "provider", "git"}
	blueprintOnlyKeys = []string{"repos", "placements", "packages", "tools", "env"}
)

// starters scaffolds a fresh config home and returns the content of the
// starter preferences and the starter blueprint.
func starters(t *testing.T) (preferences, personal string) {
	t.Helper()
	got := scaffold(t, t.TempDir())
	return readFile(t, got[0].Path), readFile(t, got[1].Path)
}

// documents reports whether content mentions key as a YAML key, commented out
// or not, at any depth.
func documents(content, key string) bool {
	return regexp.MustCompile(`(?m)^[#\s-]*` + regexp.QuoteMeta(key) + `:`).MatchString(content)
}

// declaresTopLevel reports whether content carries key as a top-level YAML
// key, commented out or not.
func declaresTopLevel(content, key string) bool {
	return regexp.MustCompile(`(?m)^(# ?)?` + regexp.QuoteMeta(key) + `:`).MatchString(content)
}

// schemaKeys returns every key the YAML schema of t accepts, at any depth.
func schemaKeys(t reflect.Type) []string {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		return schemaKeys(t.Elem())
	case reflect.Struct:
		var keys []string
		for field := range t.Fields() {
			if name, _, _ := strings.Cut(field.Tag.Get("yaml"), ","); name != "" && name != "-" {
				keys = append(keys, name)
			}
			keys = append(keys, schemaKeys(field.Type)...)
		}
		return keys
	default:
		return nil
	}
}

func TestEveryConfigurableKeyIsDocumentedInAStarter(t *testing.T) {
	preferences, personal := starters(t)

	schemas := []reflect.Type{reflect.TypeFor[blueprint.Blueprint](), reflect.TypeFor[blueprint.Preferences]()}
	for _, schema := range schemas {
		for _, key := range slices.Compact(slices.Sorted(slices.Values(schemaKeys(schema)))) {
			if !documents(preferences, key) && !documents(personal, key) {
				t.Errorf("%s key %q appears in no starter", schema.Name(), key)
			}
		}
	}
}

func TestTheStarterPreferencesDocumentTheFixedKeys(t *testing.T) {
	preferences, _ := starters(t)

	for _, key := range fixedKeys {
		if !declaresTopLevel(preferences, key) {
			t.Errorf("starter preferences do not document %q", key)
		}
	}
}

func TestTheStarterBlueprintHoldsOnlyBlueprintOnlyKeys(t *testing.T) {
	_, personal := starters(t)

	for _, key := range blueprintOnlyKeys {
		if !declaresTopLevel(personal, key) {
			t.Errorf("starter blueprint does not document %q", key)
		}
	}
	for _, key := range fixedKeys {
		if declaresTopLevel(personal, key) {
			t.Errorf("starter blueprint declares %q, want it left to the preferences", key)
		}
	}
}

// uncommentProvider returns preferences with its commented-out provider block
// uncommented: the "# provider:" line and every indented commented line after
// it.
func uncommentProvider(t *testing.T, preferences string) string {
	t.Helper()
	lines := strings.Split(preferences, "\n")
	start := slices.Index(lines, "# provider:")
	if start < 0 {
		t.Fatal("starter preferences carry no commented-out provider block")
	}
	for i := start; i < len(lines); i++ {
		if i > start && !strings.HasPrefix(lines[i], "#   ") {
			break
		}
		lines[i] = strings.TrimPrefix(lines[i], "# ")
	}
	return strings.Join(lines, "\n")
}

func TestTheStarterProviderBlockIsTheVerifiedAdapter(t *testing.T) {
	dir := t.TempDir()
	home := config.NewHome(dir)
	got := scaffold(t, dir)
	writeFile(t, got[0].Path, uncommentProvider(t, readFile(t, got[0].Path)))

	preferences, err := config.LoadPreferences(home)
	if err != nil {
		t.Fatalf("LoadPreferences(uncommented starter) err = %v, want nil", err)
	}
	verified, _, err := config.Load(home, verifiedAdapter)
	if err != nil {
		t.Fatalf("Load(%s) err = %v, want nil", verifiedAdapter, err)
	}
	if preferences.Declared.Provider == nil {
		t.Fatal("uncommented starter preferences declare no provider")
	}
	if !reflect.DeepEqual(*preferences.Declared.Provider, *verified.Provider) {
		t.Errorf("starter provider = %+v, want the verified adapter %+v", *preferences.Declared.Provider, *verified.Provider)
	}
}
