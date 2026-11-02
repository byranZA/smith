package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func homeWith(t *testing.T, files map[string]string) Home {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "blueprints"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, "blueprints", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return NewHome(dir)
}

const apiBlueprint = `repos:
  - url: https://github.com/acme/api.git
    placements:
      - from: file:~/api.env
        to: .env
        mode: once
        perms: "0600"
  - url: https://github.com/acme/web
    placements:
      - from: file:~/web.env
        to: web.env
`

func TestPlacementsForNoMatchIsEmpty(t *testing.T) {
	t.Parallel()
	home := homeWith(t, map[string]string{"a.yaml": apiBlueprint})

	got, err := PlacementsFor(home, "git@github.com:other/api.git", io.Discard)

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want nothing", got, err)
	}
}

func TestPlacementsForMissingHomeIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := PlacementsFor(NewHome(filepath.Join(t.TempDir(), "absent")), "github.com/acme/api", io.Discard)

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want nothing", got, err)
	}
}

func TestPlacementsForMatchCarriesTheDeclaringBlueprint(t *testing.T) {
	t.Parallel()
	home := homeWith(t, map[string]string{"a.yaml": apiBlueprint})

	got, err := PlacementsFor(home, "git@github.com:Acme/API.git", io.Discard)

	want := []Declared{{
		Blueprint: filepath.Join(home.Path(), "blueprints", "a.yaml"),
		Placement: blueprint.Placement{From: "file:~/api.env", To: ".env", Mode: "once", Perms: "0600"},
	}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestPlacementsForJoinsEveryBlueprintByPathPlacingEachOnce(t *testing.T) {
	t.Parallel()
	home := homeWith(t, map[string]string{
		"a.yaml": "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - {from: \"file:~/1\", to: .env, mode: converge}\n      - {from: \"file:~/2\", to: a.txt, mode: once}\n",
		"b.yaml": "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - {from: \"file:~/1\", to: .env, mode: converge}\n      - {from: \"file:~/3\", to: b.txt, mode: once}\n",
	})

	got, err := PlacementsFor(home, "https://github.com/acme/api", io.Discard)

	var paths []string
	for _, d := range got {
		paths = append(paths, d.Placement.To)
	}
	if want := []string{".env", "a.txt", "b.txt"}; err != nil || !reflect.DeepEqual(paths, want) {
		t.Errorf("got %v, %v; want %v", paths, err, want)
	}
}

func TestPlacementsForConvergeWinsWhereModesDisagree(t *testing.T) {
	t.Parallel()
	home := homeWith(t, map[string]string{
		"a.yaml": "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - {from: \"file:~/1\", to: .env, mode: once}\n",
		"b.yaml": "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - {from: \"file:~/1\", to: .env, mode: converge}\n",
	})

	got, err := PlacementsFor(home, "https://github.com/acme/api", io.Discard)

	if err != nil || len(got) != 1 || got[0].Placement.Mode != "converge" {
		t.Errorf("got %+v, %v; want one converge placement", got, err)
	}
}

func TestPlacementsForWarnsOfAnUnreadableBlueprintAndGoesOn(t *testing.T) {
	t.Parallel()
	home := homeWith(t, map[string]string{
		"bad.yaml": "repos: [unclosed",
		"ok.yaml":  "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - {from: \"file:~/1\", to: .env, mode: once}\n",
	})
	var warn bytes.Buffer

	got, err := PlacementsFor(home, "https://github.com/acme/api", &warn)

	bad := filepath.Join(home.Path(), "blueprints", "bad.yaml")
	if err != nil || len(got) != 1 || strings.Count(warn.String(), "\n") != 1 ||
		!strings.HasPrefix(warn.String(), "smith: warning: ") || !strings.Contains(warn.String(), bad) {
		t.Errorf("got %+v, %v, warning %q; want the ok placement and one warning naming %s", got, err, warn.String(), bad)
	}
}
