package staging

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/secret"
)

func TestBoxPlacementPathFlattensTheDestination(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		want        string
	}{
		{"an absolute box path", "/home/smith/.npmrc", "/etc/smith/placements/box/%2Fhome%2Fsmith%2F.npmrc"},
		{"a tilde-relative path canonicalizes to the smith home", "~/.npmrc", "/etc/smith/placements/box/%2Fhome%2Fsmith%2F.npmrc"},
		{"a percent in the destination is encoded first", "/tmp/a%2Fb", "/etc/smith/placements/box/%2Ftmp%2Fa%252Fb"},
		{"a nested destination stays one flat key", "/etc/app/conf.d/app.conf", "/etc/smith/placements/box/%2Fetc%2Fapp%2Fconf.d%2Fapp.conf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoxPlacementPathIn(Root, tt.destination); got != tt.want {
				t.Errorf("BoxPlacementPathIn(%q) = %q, want %q", tt.destination, got, tt.want)
			}
		})
	}
}

func TestBoxPlacementPathDistinguishesADestinationFromItsEncoding(t *testing.T) {
	slashed := BoxPlacementPathIn(Root, "/a/b")
	literal := BoxPlacementPathIn(Root, "/a%2Fb")
	if slashed == literal {
		t.Errorf("%q and %q both key to %q, want distinct staged files", "/a/b", "/a%2Fb", slashed)
	}
}

func TestPlanKeysABoxPlacementWhereTheReaderLooksForIt(t *testing.T) {
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "file:/home/op/.secrets/npmrc", To: "/home/smith/.npmrc"}}}
	tree := Plan([]byte("access: public\n"), b)
	if len(tree.Placements) != 1 {
		t.Fatalf("Plan() planned %d placements, want 1", len(tree.Placements))
	}
	if got, want := tree.Placements[0].File.Path, BoxPlacementPathIn(Root, "/home/smith/.npmrc"); got != want {
		t.Errorf("writer staged at %q, reader looks at %q", got, want)
	}
}

func TestPlanKeysBothSpellingsOfABoxDestinationIdentically(t *testing.T) {
	tilde := Plan(nil, blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:T", To: "~/.npmrc"}}})
	absolute := Plan(nil, blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:T", To: "/home/smith/.npmrc"}}})
	if tilde.Placements[0].File.Path != absolute.Placements[0].File.Path {
		t.Errorf("~/.npmrc staged at %q but /home/smith/.npmrc at %q, want one key",
			tilde.Placements[0].File.Path, absolute.Placements[0].File.Path)
	}
}

func TestPlanGivesAPlacementItsDeclaredPerms(t *testing.T) {
	tests := []struct {
		name  string
		perms string
		want  string
	}{
		{"declared perms are carried", "0644", "0644"},
		{"an undeclared placement defaults to 0600", "", "0600"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := Plan(nil, blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:T", To: "/home/smith/.npmrc", Perms: tt.perms}}})
			if got := tree.Placements[0].File.Mode; got != tt.want {
				t.Errorf("placement mode = %q, want %q", got, tt.want)
			}
			if got := tree.Placements[0].File.Owner; got != "smith:smith" {
				t.Errorf("placement owner = %q, want the smith user", got)
			}
		})
	}
}

func TestPlanAlwaysDeclaresThePlacementsDirectorySmithOwnedAt0700(t *testing.T) {
	tree := Plan([]byte("access: public\n"), blueprint.Blueprint{})
	var found bool
	for _, d := range tree.Dirs {
		if d.Path != PlacementsDir {
			continue
		}
		found = true
		if d.Mode != "0700" || d.Owner != "smith:smith" {
			t.Errorf("%s is %s %s, want 0700 smith:smith", d.Path, d.Mode, d.Owner)
		}
	}
	if !found {
		t.Errorf("Plan() declared dirs %v, want %s among them even with no placements", tree.Dirs, PlacementsDir)
	}
}

func TestResolveAttachesTheBytesEachPlacementSourceHolds(t *testing.T) {
	t.Setenv("NPM_TOKEN", "//registry.npmjs.org/:_authToken=s3cr3t")
	tree := Plan(nil, blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "~/.npmrc"}}})

	resolved, err := Resolve(tree, secret.Resolve)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := string(resolved.Placements[0].File.Bytes); got != "//registry.npmjs.org/:_authToken=s3cr3t" {
		t.Errorf("staged bytes = %q, want the value of NPM_TOKEN", got)
	}
	if len(tree.Placements[0].File.Bytes) != 0 {
		t.Error("Resolve() wrote bytes back into the plan it was given, want a plan of its own")
	}
}

func TestResolveNamesAReferenceItCannotResolve(t *testing.T) {
	tree := Plan(nil, blueprint.Blueprint{Placements: []blueprint.Placement{{From: "file:/home/op/.missing", To: "/home/smith/.npmrc"}}})

	_, err := Resolve(tree, secret.Resolve)
	if err == nil {
		t.Fatal("Resolve() error = nil, want the unresolvable reference refused")
	}
	for _, want := range []string{"file:/home/op/.missing", "/home/smith/.npmrc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error = %v, want it to name %q", err, want)
		}
	}
}
