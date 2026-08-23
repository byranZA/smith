package blueprint_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestParseChecksTheSchemeOfAnEnvValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"env reference", "env:GH_TOKEN", true},
		{"file reference", "file:~/.secrets/tok", true},
		{"deliberate literal", "literal:debug", true},
		{"bare value", "ghp_realtokenhere", false},
		{"unknown scheme", "json:pretty", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := map[string]string{
				"box scope":  "env:\n  VALUE: " + tt.value + "\n",
				"repo scope": "repos:\n  - url: git@github.com:acme/api.git\n    env:\n      VALUE: " + tt.value + "\n",
			}
			for scope, doc := range docs {
				_, err := blueprint.Parse([]byte(doc))
				if tt.valid && err != nil {
					t.Errorf("Parse(%s) err = %v, want the reference accepted", scope, err)
				}
				if !tt.valid && err == nil {
					t.Errorf("Parse(%s) err = nil, want the value refused as no reference", scope)
				}
			}
		})
	}
}

func TestParseNamesAnUnrecognisedSchemeAsSuch(t *testing.T) {
	got := findings(t, "env:\n  LOG_FORMAT: json:pretty\n")
	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Path != "env.LOG_FORMAT" {
		t.Errorf("Path = %q, want the variable named", got[0].Path)
	}
	for _, want := range []string{`"json"`, "env:", "file:", "literal:"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("Message = %q, want it to mention %s", got[0].Message, want)
		}
	}
}

func TestParseRefusesAValueWithNoSchemeAsALiteral(t *testing.T) {
	got := findings(t, "env:\n  GITHUB_TOKEN: ghp_realtokenhere\n")
	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if !strings.Contains(got[0].Message, "not a reference") {
		t.Errorf("Message = %q, want it to say the value is not a reference", got[0].Message)
	}
	if !strings.Contains(got[0].Message, "literal:") {
		t.Errorf("Message = %q, want it to name literal: as the deliberate way to declare a plain value", got[0].Message)
	}
	if strings.Contains(got[0].Message, "ghp_realtokenhere") {
		t.Errorf("Message = %q, want the suspected secret kept out of the report", got[0].Message)
	}
}

func TestParseChecksTheSchemeOfAPlacementSource(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		valid bool
	}{
		{"file source", "file:~/.secrets/api.env", true},
		{"env source", "env:API_ENV", true},
		{"literal source", "literal:hello", false},
		{"bare source", "~/.secrets/api.env", false},
		{"unknown scheme", "json:pretty", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := "placements:\n  - from: " + tt.from + "\n    to: ~/a\n"
			_, err := blueprint.Parse([]byte(doc))
			if tt.valid && err != nil {
				t.Fatalf("Parse() err = %v, want the source accepted", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("Parse() err = nil, want the source refused")
			}
		})
	}
}

func TestParseNamesLiteralAsInvalidInAPlacementSource(t *testing.T) {
	got := findings(t, "repos:\n  - url: git@github.com:acme/api.git\n    placements:\n      - from: literal:hello\n        to: .env\n")
	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Path != "repos[0].placements[0].from" {
		t.Errorf("Path = %q, want the placement named by scope and index", got[0].Path)
	}
	if !strings.Contains(got[0].Message, "literal:") || !strings.Contains(got[0].Message, "source") {
		t.Errorf("Message = %q, want it to say literal: is not valid as a placement source", got[0].Message)
	}
}

func TestParseResolvesNoReference(t *testing.T) {
	const doc = `env:
  MISSING: env:SMITH_NO_SUCH_VARIABLE_EXISTS
placements:
  - from: file:/no/such/path/anywhere
    to:   ~/a
`
	if _, err := blueprint.Parse([]byte(doc)); err != nil {
		t.Fatalf("Parse() err = %v, want references checked for scheme and never read", err)
	}
}

func TestParseSplitsAReferenceOnTheFirstColonOnly(t *testing.T) {
	const doc = "env:\n  KEY: file:C:\\keys\\ts\n"
	if _, err := blueprint.Parse([]byte(doc)); err != nil {
		t.Fatalf("Parse() err = %v, want a Windows-style path to survive the split", err)
	}
}

// The ssh key reference is the one field where a bare literal and a known
// scheme must both stay legal: Hetzner wants a key name, Linode wants the raw
// public key line, and a public key is not a secret. So the scheme rule does
// not reach it, and Parse leaves whatever the operator wrote alone.
func TestParseChecksNoSchemeOnTheProviderSSHKey(t *testing.T) {
	tests := []struct {
		name   string
		sshKey string
	}{
		{"a key name registered at the provider", "my-laptop"},
		{"a raw public key line", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH8Q ada@laptop"},
		{"a value that reads like a reference scheme", "env:SSH_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := "provider:\n  create: [hcloud, server, create]\n  ssh_key: \"" + tt.sshKey + "\"\n"

			b, err := blueprint.Parse([]byte(doc))
			if err != nil {
				t.Fatalf("Parse() err = %v, want the ssh key reference accepted as written", err)
			}
			if b.Provider.SSHKey != tt.sshKey {
				t.Errorf("Provider.SSHKey = %q, want %q unaltered", b.Provider.SSHKey, tt.sshKey)
			}
		})
	}
}

// TestValueResolvesTheSchemesABlueprintValueMayName drives the resolution of a
// declared value, which is the wider set a placement source has: a literal is
// a plain value declared on purpose and resolves to itself.
func TestValueResolvesTheSchemesABlueprintValueMayName(t *testing.T) {
	t.Setenv("SMITH_TEST_TOKEN", "ghp_fromtheenvironment")
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "a literal is the value itself", ref: "literal:production", want: "production"},
		{name: "a literal keeps a colon in its value", ref: "literal:json:pretty", want: "json:pretty"},
		{name: "an env reference reads the variable", ref: "env:SMITH_TEST_TOKEN", want: "ghp_fromtheenvironment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := blueprint.Value(tt.ref)
			if err != nil {
				t.Fatalf("blueprint.Value(%q) error = %v, want nil", tt.ref, err)
			}
			if got != tt.want {
				t.Errorf("blueprint.Value(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// TestValueRefusesWhatIsNotAReference proves the resolver refuses the same
// values the schema does, rather than passing a bare string through as though
// the operator had declared it on purpose.
func TestValueRefusesWhatIsNotAReference(t *testing.T) {
	for _, ref := range []string{"ghp_abc", "vault:token"} {
		if got, err := blueprint.Value(ref); err == nil {
			t.Errorf("blueprint.Value(%q) = %q, want a refusal", ref, got)
		}
	}
}

// TestValueRefusesAnEmptyLiteral proves a literal with nothing after the colon
// is a refusal rather than an empty export, which would surface later as a
// variable set to nothing.
func TestValueRefusesAnEmptyLiteral(t *testing.T) {
	if got, err := blueprint.Value("literal:"); err == nil {
		t.Errorf("blueprint.Value(%q) = %q, want a refusal", "literal:", got)
	}
}
