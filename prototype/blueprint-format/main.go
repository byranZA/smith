// PROTOTYPE — throwaway. Answers smith#69: which file format carries the
// blueprint schema locked by #62, and what does strict validation's failure
// surface actually look like?
//
// Not production code. No abstractions, no tests, no error wrapping.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

// ---- the #62 schema, transcribed ----

type Blueprint struct {
	Access     string            `yaml:"access"     toml:"access"`
	Terminal   string            `yaml:"terminal"   toml:"terminal"`
	Workspace  string            `yaml:"workspace"  toml:"workspace"`
	Provider   *Provider         `yaml:"provider"   toml:"provider"`
	Packages   []string          `yaml:"packages"   toml:"packages"`
	Tools      map[string]string `yaml:"tools"      toml:"tools"`
	Env        map[string]string `yaml:"env"        toml:"env"`
	Placements []Placement       `yaml:"placements" toml:"placements"`
	Repos      []Repo            `yaml:"repos"      toml:"repos"`
}

type Provider struct {
	Create   []string          `yaml:"create"   toml:"create"`
	List     []string          `yaml:"list"     toml:"list"`
	Destroy  []string          `yaml:"destroy"  toml:"destroy"`
	Requires []string          `yaml:"requires" toml:"requires"`
	Marker   map[string]string `yaml:"marker"   toml:"marker"`
	Extract  map[string]string `yaml:"extract"  toml:"extract"`
}

type Placement struct {
	From  string `yaml:"from"  toml:"from"`
	To    string `yaml:"to"    toml:"to"`
	Mode  string `yaml:"mode"  toml:"mode"`
	Perms string `yaml:"perms" toml:"perms"`
}

type Repo struct {
	Name       string            `yaml:"name"       toml:"name"`
	URL        string            `yaml:"url"        toml:"url"`
	Base       string            `yaml:"base"       toml:"base"`
	Tools      map[string]string `yaml:"tools"      toml:"tools"`
	Env        map[string]string `yaml:"env"        toml:"env"`
	Placements []Placement       `yaml:"placements" toml:"placements"`
}

// ---- semantic validation on top of the parser's structural pass ----

// Schema field names, reserved inside the open maps (`tools`, `env`) so a
// misindented `base:`/`url:` errors instead of becoming a phantom entry.
// Case-sensitive on purpose: a real env var named URL or MODE is untouched.
var reserved = map[string]bool{
	"access": true, "terminal": true, "workspace": true, "provider": true,
	"packages": true, "tools": true, "env": true, "placements": true,
	"repos": true, "name": true, "url": true, "base": true,
	"from": true, "to": true, "mode": true, "perms": true,
}

func validate(b *Blueprint) []string {
	var errs []string

	checkOpenMap := func(where string, m map[string]string) {
		for k := range m {
			if reserved[k] {
				errs = append(errs, fmt.Sprintf("%s.%s: %q is a schema field name and is reserved here — check the indentation", where, k, k))
			}
		}
	}

	checkRef := func(where, v string, allowLiteral bool) {
		scheme, _, ok := strings.Cut(v, ":")
		switch {
		case !ok:
			errs = append(errs, fmt.Sprintf("%s: %q is not a reference — expected file:, env:%s (#67: values are references, never literals)",
				where, v, map[bool]string{true: " or literal:", false: ""}[allowLiteral]))
		case scheme == "literal" && !allowLiteral:
			errs = append(errs, fmt.Sprintf("%s: literal: is not valid in a placement `from` (#67)", where))
		case scheme != "file" && scheme != "env" && scheme != "literal":
			errs = append(errs, fmt.Sprintf("%s: unknown reference scheme %q — known: file:, env:%s",
				where, scheme, map[bool]string{true: ", literal:", false: ""}[allowLiteral]))
		}
	}
	// Scope is by declaration site (#62). Make the convention a rule: a repo
	// placement is worktree-relative, a box placement is absolute.
	checkPlacements := func(where string, ps []Placement, repoScope bool) {
		for i, p := range ps {
			at := fmt.Sprintf("%s[%d]", where, i)
			checkRef(at+".from", p.From, false)
			abs := strings.HasPrefix(p.To, "/") || strings.HasPrefix(p.To, "~")
			switch {
			case repoScope && abs:
				errs = append(errs, fmt.Sprintf("%s.to: %q is absolute, but a repo placement is worktree-relative — declare it at box scope to write outside the worktree", at, p.To))
			case !repoScope && !abs:
				errs = append(errs, fmt.Sprintf("%s.to: %q is relative, but a box placement needs an absolute or ~-relative path", at, p.To))
			}
			if p.Mode != "" && p.Mode != "converge" && p.Mode != "once" {
				errs = append(errs, fmt.Sprintf("%s.mode: %q is not one of converge, once", at, p.Mode))
			}
		}
	}

	checkOpenMap("tools", b.Tools)
	checkOpenMap("env", b.Env)
	for k, v := range b.Env {
		checkRef("env."+k, v, true)
	}
	checkPlacements("placements", b.Placements, false)
	for i, r := range b.Repos {
		where := fmt.Sprintf("repos[%d]", i)
		if r.URL == "" {
			errs = append(errs, where+".url: required")
		}
		checkOpenMap(where+".tools", r.Tools)
		checkOpenMap(where+".env", r.Env)
		for k, v := range r.Env {
			checkRef(fmt.Sprintf("%s.env.%s", where, k), v, true)
		}
		checkPlacements(where+".placements", r.Placements, true)
	}
	if b.Terminal != "" && b.Terminal != "tmux" {
		errs = append(errs, fmt.Sprintf("terminal: %q is not recognized in v1 — only tmux (#64)", b.Terminal))
	}
	return errs
}

// Preferences: fixed-key fields only (#62) — no collections.
type Preferences struct {
	Access    string    `yaml:"access"    toml:"access"`
	Terminal  string    `yaml:"terminal"  toml:"terminal"`
	Workspace string    `yaml:"workspace" toml:"workspace"`
	Provider  *Provider `yaml:"provider"  toml:"provider"`
}

// ---- strict loaders ----

func loadYAML(path string) (*Blueprint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true) // strict: unknown key is an error
	var b Blueprint
	return &b, d.Decode(&b)
}

func loadTOML(path string) (*Blueprint, error) {
	var b Blueprint
	md, err := toml.DecodeFile(path, &b)
	if err != nil {
		return nil, err
	}
	if u := md.Undecoded(); len(u) > 0 { // strict: pushed onto the caller
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return &b, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	return &b, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "resolve" {
		fmt.Println("\n=== name selection under home/blueprints/ ===")
		resolveDemo("home/blueprints", []string{"acme", "prod", "staging", "dev"})
		return
	}
	for _, path := range os.Args[1:] {
		fmt.Printf("\n=== %s ===\n", path)
		if strings.Contains(path, "preferences") {
			checkPrefs(path)
			continue
		}
		var b *Blueprint
		var err error
		if strings.HasSuffix(path, ".toml") {
			b, err = loadTOML(path)
		} else {
			b, err = loadYAML(path)
		}
		if err != nil {
			fmt.Printf("parse error:\n  %s\n", strings.ReplaceAll(err.Error(), "\n", "\n  "))
		}
		if b == nil {
			continue
		}
		for _, e := range validate(b) {
			fmt.Printf("validation error:\n  %s\n", e)
		}
		if err == nil {
			fmt.Printf("ok — %d repo(s), %d box placement(s), %d tool(s), %d env var(s)\n",
				len(b.Repos), len(b.Placements), len(b.Tools), len(b.Env))
			for i, r := range b.Repos {
				name := r.Name
				if name == "" {
					name = "(derived from url)"
				}
				fmt.Printf("  repos[%d] %-24s base=%-10s tools=%v placements=%d\n",
					i, name, orDash(r.Base), r.Tools, len(r.Placements))
			}
		}
	}
}

func checkPrefs(path string) {
	var p Preferences
	var err error
	if strings.HasSuffix(path, ".toml") {
		var md toml.MetaData
		md, err = toml.DecodeFile(path, &p)
		if err == nil {
			if u := md.Undecoded(); len(u) > 0 {
				keys := make([]string, len(u))
				for i, k := range u {
					keys[i] = k.String()
				}
				err = fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
			}
		}
	} else {
		f, e := os.Open(path)
		if e != nil {
			fmt.Println(e)
			return
		}
		defer f.Close()
		d := yaml.NewDecoder(f)
		d.KnownFields(true)
		err = d.Decode(&p)
	}
	if err != nil {
		fmt.Printf("parse error:\n  %s\n", strings.ReplaceAll(err.Error(), "\n", "\n  "))
		return
	}
	fmt.Printf("ok — access=%s terminal=%s workspace=%s provider=%t\n",
		orDash(p.Access), orDash(p.Terminal), orDash(p.Workspace), p.Provider != nil)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
