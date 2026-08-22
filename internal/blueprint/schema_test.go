package blueprint_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// findings parses doc and returns the report it was refused with.
func findings(t *testing.T, doc string) []blueprint.Finding {
	t.Helper()
	_, err := blueprint.Parse([]byte(doc))
	var invalid *blueprint.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Parse() err = %v (%T), want a validation report", err, err)
	}
	return invalid.Findings
}

func TestParseDecodesEverySchemaField(t *testing.T) {
	data, err := os.ReadFile("testdata/acme.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("Parse(acme.yaml) err = %v, want nil", err)
	}

	if got.Access != "tailscale" || got.Terminal != "tmux" || got.Workspace != "~/workspace" {
		t.Errorf("access/terminal/workspace = %q/%q/%q, want tailscale/tmux/~/workspace", got.Access, got.Terminal, got.Workspace)
	}
	if got.Git.UserName != "Ada Lovelace" || got.Git.UserEmail != "ada@acme.example" {
		t.Errorf("Git = %+v, want the declared identity", got.Git)
	}
	if got.Provider == nil {
		t.Fatal("Provider = nil, want the declared provider block")
	}
	p := got.Provider
	if len(p.Create) != 11 || p.Create[0] != "hcloud" {
		t.Errorf("Provider.Create = %v, want the eleven-argument hcloud template", p.Create)
	}
	if len(p.List) != 5 || len(p.Destroy) != 6 {
		t.Errorf("Provider.List/Destroy = %v/%v, want both templates decoded", p.List, p.Destroy)
	}
	if len(p.Requires) != 1 || p.Requires[0] != "HCLOUD_TOKEN" {
		t.Errorf("Provider.Requires = %v, want [HCLOUD_TOKEN]", p.Requires)
	}
	if p.SSHKey != "acme-box" {
		t.Errorf("Provider.SSHKey = %q, want %q", p.SSHKey, "acme-box")
	}
	if p.Marker.Arg != "smith={{value}}" || p.Marker.Read != "labels.smith" {
		t.Errorf("Provider.Marker = %+v, want the declared marker", p.Marker)
	}
	if p.Extract.ID != "id" || p.Extract.IP != "public_net.ipv4.ip" {
		t.Errorf("Provider.Extract = %+v, want the declared extractors", p.Extract)
	}
	if len(got.Packages) != 3 || got.Packages[0] != "ripgrep" {
		t.Errorf("Packages = %v, want the three declared packages", got.Packages)
	}
	if got.Tools["node"] != "20" || got.Tools["go"] != "1.23" {
		t.Errorf("Tools = %v, want node 20 and go 1.23", got.Tools)
	}
	if got.Env["GITHUB_TOKEN"] != "env:GH_TOKEN" || got.Env["LOG_LEVEL"] != "literal:debug" {
		t.Errorf("Env = %v, want the declared references", got.Env)
	}
	if len(got.Placements) != 1 {
		t.Fatalf("Placements = %v, want one box placement", got.Placements)
	}
	box := got.Placements[0]
	if box.From != "file:~/.secrets/acme/id_forge" || box.To != "~/.ssh/id_forge" || box.Mode != "converge" || box.Perms != "0600" {
		t.Errorf("Placements[0] = %+v, want the declared box placement", box)
	}
	if len(got.Repos) != 2 {
		t.Fatalf("Repos = %v, want two repos", got.Repos)
	}
	api := got.Repos[0]
	if api.Name != "acme-api" || api.URL != "git@github.com:acme/api.git" || api.Base != "develop" {
		t.Errorf("Repos[0] = %+v, want the declared name, url and base", api)
	}
	if api.Tools["node"] != "22" || api.Env["ACME_REGION"] != "env:ACME_REGION" {
		t.Errorf("Repos[0] tools/env = %v/%v, want the per-repo overrides", api.Tools, api.Env)
	}
	if len(api.Placements) != 2 || api.Placements[1].To != "seed.sql" || api.Placements[1].Mode != "once" {
		t.Errorf("Repos[0].Placements = %+v, want both repo placements", api.Placements)
	}
}

func TestParseRequiresARepoURL(t *testing.T) {
	got := findings(t, "repos:\n  - name: api\n")

	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Path != "repos[0]" {
		t.Errorf("Path = %q, want %q so the report names the repo", got[0].Path, "repos[0]")
	}
	if !strings.Contains(got[0].Message, "url") {
		t.Errorf("Message = %q, want it to name the missing url", got[0].Message)
	}
}

func TestParseDefaultsRepoNameFromItsURL(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"ssh url with a .git suffix", "repos:\n  - url: git@github.com:acme/api.git\n", "api"},
		{"https url without a suffix", "repos:\n  - url: https://github.com/acme/web\n", "web"},
		{"a declared name wins", "repos:\n  - name: acme-api\n    url: git@github.com:acme/api.git\n", "acme-api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := blueprint.Parse([]byte(tt.doc))
			if err != nil {
				t.Fatalf("Parse() err = %v, want nil", err)
			}
			if len(got.Repos) != 1 {
				t.Fatalf("Repos = %+v, want one", got.Repos)
			}
			if got.Repos[0].Name != tt.want {
				t.Errorf("Repos[0].Name = %q, want %q", got.Repos[0].Name, tt.want)
			}
		})
	}
}

func TestParseRefusesTwoReposResolvingToOneName(t *testing.T) {
	const doc = `repos:
  - name: api
    url:  git@github.com:acme/api.git
  - url:  git@gitlab.com:other/api.git
`

	got := findings(t, doc)

	if len(got) != 1 {
		t.Fatalf("Findings = %+v, want exactly one", got)
	}
	if got[0].Path != "repos[1]" {
		t.Errorf("Path = %q, want %q so the report names the second repo", got[0].Path, "repos[1]")
	}
	if !strings.Contains(got[0].Message, `"api"`) {
		t.Errorf("Message = %q, want it to name the colliding name", got[0].Message)
	}
	if !strings.Contains(got[0].Message, "workspace") {
		t.Errorf("Message = %q, want it to name the workspace collision as the reason", got[0].Message)
	}
}

func TestParseConstrainsFieldsToTheirKnownValues(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		valid bool
	}{
		{"access public", "access: public\n", true},
		{"access tailscale", "access: tailscale\n", true},
		{"access wireguard", "access: wireguard\n", false},
		{"terminal tmux", "terminal: tmux\n", true},
		{"terminal zellij", "terminal: zellij\n", false},
		{"mode converge", "placements:\n  - from: file:~/a\n    to: ~/a\n    mode: converge\n", true},
		{"mode once", "placements:\n  - from: file:~/a\n    to: ~/a\n    mode: once\n", true},
		{"mode replace", "placements:\n  - from: file:~/a\n    to: ~/a\n    mode: replace\n", false},
		{"mode unset", "placements:\n  - from: file:~/a\n    to: ~/a\n", true},
		{"perms octal", "placements:\n  - from: file:~/a\n    to: ~/a\n    perms: \"0600\"\n", true},
		{"perms three digits", "placements:\n  - from: file:~/a\n    to: ~/a\n    perms: \"644\"\n", true},
		{"perms symbolic", "placements:\n  - from: file:~/a\n    to: ~/a\n    perms: rw-------\n", false},
		{"perms out of range", "placements:\n  - from: file:~/a\n    to: ~/a\n    perms: \"0900\"\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := blueprint.Parse([]byte(tt.doc))
			if tt.valid && err != nil {
				t.Fatalf("Parse(%q) err = %v, want nil", tt.doc, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("Parse(%q) err = nil, want the value refused", tt.doc)
			}
		})
	}
}

func TestParseNamesWhatAConstrainedFieldAccepts(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
		want string
	}{
		{"terminal names v1's only substrate", "terminal: zellij\n", "terminal", "tmux"},
		{"access names both modes", "access: wireguard\n", "access", "tailscale"},
		{"mode names both modes", "placements:\n  - from: file:~/a\n    to: ~/a\n    mode: replace\n", "placements[0].mode", "converge"},
		{"perms names the octal shape", "placements:\n  - from: file:~/a\n    to: ~/a\n    perms: rw-------\n", "placements[0].perms", "0600"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.doc)
			if len(got) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", got)
			}
			if got[0].Path != tt.path {
				t.Errorf("Path = %q, want %q", got[0].Path, tt.path)
			}
			if !strings.Contains(got[0].Message, tt.want) {
				t.Errorf("Message = %q, want it to name %q", got[0].Message, tt.want)
			}
		})
	}
}

func TestParseReportsSemanticFindingsBesideSchemaFindings(t *testing.T) {
	const doc = `terminals: tmux
access:    wireguard
repos:
  - name: api
`

	got := findings(t, doc)

	if len(got) != 3 {
		t.Fatalf("Findings = %+v, want all three problems in one run", got)
	}
	report := strings.Join([]string{got[0].String(), got[1].String(), got[2].String()}, "\n")
	for _, want := range []string{"terminals", "wireguard", "repos[0]"} {
		if !strings.Contains(report, want) {
			t.Errorf("report = %q, want it to mention %q", report, want)
		}
	}
}
