package config

import (
	"os"
	"path/filepath"
	"reflect"
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
	home := homeWith(t, map[string]string{"a.yaml": apiBlueprint})

	got, err := PlacementsFor(home, "git@github.com:other/api.git")

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want nothing", got, err)
	}
}

func TestPlacementsForMissingHomeIsEmpty(t *testing.T) {
	got, err := PlacementsFor(NewHome(filepath.Join(t.TempDir(), "absent")), "github.com/acme/api")

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want nothing", got, err)
	}
}

func TestPlacementsForMatchCarriesTheDeclaringBlueprint(t *testing.T) {
	home := homeWith(t, map[string]string{"a.yaml": apiBlueprint})

	got, err := PlacementsFor(home, "git@github.com:Acme/API.git")

	want := []Declared{{
		Blueprint: filepath.Join(home.Path(), "blueprints", "a.yaml"),
		Placement: blueprint.Placement{From: "file:~/api.env", To: ".env", Mode: "once", Perms: "0600"},
	}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v; want %+v", got, err, want)
	}
}
