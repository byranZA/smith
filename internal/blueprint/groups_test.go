package blueprint_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParseReadsTheDeclaredGroups(t *testing.T) {
	got, err := blueprint.Parse([]byte("groups:\n  - docker\n  - kvm\n"))
	if err != nil {
		t.Fatalf("Parse() err = %v, want nil", err)
	}
	if want := []string{"docker", "kvm"}; !reflect.DeepEqual(got.Groups, want) {
		t.Errorf("Parse().Groups = %v, want %v", got.Groups, want)
	}
}

func TestParseAcceptsGroupNamesAdduserAllows(t *testing.T) {
	for _, name := range []string{"docker", "kvm", "_ssh", "plugdev", "sys-admin", "lp2", "machine$"} {
		t.Run(name, func(t *testing.T) {
			if _, err := blueprint.Parse([]byte("groups:\n  - \"" + name + "\"\n")); err != nil {
				t.Errorf("Parse(groups [%q]) err = %v, want it accepted", name, err)
			}
		})
	}
}

func TestParseRefusesAMalformedGroupName(t *testing.T) {
	for _, name := range []string{"Docker", "2fa", "-docker", "doc ker", "docker:x", "", "do$cker"} {
		t.Run(name, func(t *testing.T) {
			got := findings(t, "groups:\n  - docker\n  - \""+name+"\"\n")
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", got)
			}
			if got[0].Path != "groups[1]" {
				t.Errorf("Path = %q, want %q so the report names the entry", got[0].Path, "groups[1]")
			}
			if !strings.Contains(got[0].Message, "\""+name+"\"") {
				t.Errorf("Message = %q, want it to name %q", got[0].Message, name)
			}
		})
	}
}

func TestParseRefusesADuplicatedGroup(t *testing.T) {
	got := findings(t, "groups:\n  - docker\n  - kvm\n  - docker\n")
	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Path != "groups[2]" {
		t.Errorf("Path = %q, want %q so the report names the repeat", got[0].Path, "groups[2]")
	}
	if !strings.Contains(got[0].Message, `"docker"`) || !strings.Contains(got[0].Message, "twice") {
		t.Errorf("Message = %q, want it to name %q as declared twice", got[0].Message, "docker")
	}
}
