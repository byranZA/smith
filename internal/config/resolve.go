package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
)

// The built-in defaults: what smith uses for a field nobody declared. They are
// the last link of the precedence chain, so every resolved field always has a
// value and no caller ever has to ask what an unset one means.
const (
	defaultAccess    = "public"
	defaultTerminal  = "tmux"
	defaultWorkspace = "~/workspace"
)

// Origin is where a resolved value came from. It is returned data rather than
// something printed on the way past, so precedence is a fact a caller can
// assert on and the command surface stays a formatter.
type Origin string

// The origins a resolved value can have, most specific first. They are worded
// as the operator would say them, because they are what the resolved
// configuration reports.
const (
	// FromFlag is a value the operator named on the command line.
	FromFlag Origin = "flag"
	// FromBlueprint is a value the blueprint declared.
	FromBlueprint Origin = "blueprint"
	// FromBlueprintReplacing is a blueprint's provider block standing in place
	// of the one the preferences declared. It is worded apart from
	// FromBlueprint because an operator reading it needs to see that their
	// preference adapter is not in play at all, rather than assume the two
	// were merged.
	FromBlueprintReplacing Origin = "blueprint, replacing the preference"
	// FromPreferences is a value the operator's preferences declared.
	FromPreferences Origin = "preferences"
	// FromDefault is a value nobody declared, filled in by smith.
	FromDefault Origin = "built-in default"
)

// Overrides are the values the operator named on the command line, which
// outrank every declared one. An unset field is not an override.
type Overrides struct {
	// Access is how the box is reached: "public" or "tailscale".
	Access string
}

// Value is one resolved field: what smith would use, and where that came from.
type Value struct {
	// Value is the resolved value.
	Value string
	// Origin is where the resolved value came from.
	Origin Origin
}

// String renders a resolved value as what smith would use followed by where it
// came from, so a reader never has to hold the precedence chain in their head.
func (v Value) String() string { return fmt.Sprintf("%s (%s)", v.Value, v.Origin) }

// Adapter is the resolved provider block: the whole adapter smith would use,
// and where it came from. It is resolved as a block rather than field by
// field, because an adapter is a coherent description of one provider's CLI —
// half of one and half of another describes no provider that exists, and the
// mismatch would surface only at box creation as a permission error naming
// nothing useful.
//
// A nil Provider is a run with no adapter declared anywhere. There is no
// built-in one to fall back to, so it carries no origin.
type Adapter struct {
	// Provider is the adapter smith would use, or nil when nobody declared one.
	Provider *blueprint.Provider
	// Origin is where the adapter came from.
	Origin Origin
}

// String renders the resolved adapter as the command its templates run
// followed by where it came from. An adapter carries no name of its own, and
// the command is the part of it an operator recognises at a glance.
func (a Adapter) String() string {
	if a.Provider == nil {
		return "none declared"
	}
	return fmt.Sprintf("%s (%s)", adapterCommand(a.Provider), a.Origin)
}

// adapterCommand is the provider CLI an adapter drives, read off the create
// template. An adapter with no create template is named by nothing, so it is
// reported as declared and left at that.
func adapterCommand(p *blueprint.Provider) string {
	if len(p.Create) == 0 {
		return "declared"
	}
	return p.Create[0]
}

// Identity is the resolved git identity the box commits as: each field
// carrying where it came from. It is resolved field by field rather than
// wholesale like the adapter, because a name and an address are independent —
// a blueprint pinning the name of a shared bot leaves the operator's own
// address standing.
//
// smith has no built-in identity to fall back to, so a field nobody declared
// stays unset and carries no origin. An invented one would be worse than none:
// it would put a wrong author on every commit the box makes.
type Identity struct {
	// UserName is the name commits are authored under.
	UserName Value
	// UserEmail is the address commits are authored under.
	UserEmail Value
}

// lines renders the declared halves of the identity, one per line. An
// undeclared half is left out rather than shown empty.
func (i Identity) lines() []string {
	var out []string
	for _, half := range []struct {
		name  string
		value Value
	}{
		{"user_name", i.UserName},
		{"user_email", i.UserEmail},
	} {
		if half.value.Origin != "" {
			out = append(out, fmt.Sprintf("%-11s %s", half.name+":", half.value))
		}
	}
	return out
}

// Effective is the configuration smith would actually use — every field
// resolved through the precedence chain, each carrying its origin. It answers
// the question an operator checking a blueprint is really asking: not "is the
// document shaped right?" but "what would setup do?".
type Effective struct {
	// Access is how the box is reached.
	Access Value
	// Terminal is the terminal substrate sessions run under.
	Terminal Value
	// Workspace is the directory on the box the repo worktrees live under.
	Workspace Value
	// Provider is the adapter smith creates and lists boxes through.
	Provider Adapter
	// Git is the identity the box commits as.
	Git Identity
	// Repos are the repositories the box carries, each named — the operator's
	// own name, or the one defaulted from its clone URL.
	Repos []blueprint.Repo
	// Packages are the system packages installed on the box.
	Packages []string
	// Tools are the box-wide toolchain versions, keyed by tool name.
	Tools map[string]string
	// Env are the box-wide environment variables, keyed by variable name.
	Env map[string]string
	// Placements are the files put on the box outside any worktree.
	Placements []blueprint.Placement
}

// String renders the resolved configuration as one line per field in schema
// order, with the collections the blueprint declared written out under theirs.
// A field nobody declared and smith has no default for is left out entirely,
// so what is printed is what setup would act on and nothing else.
func (e Effective) String() string {
	lines := []string{
		field("access", e.Access),
		field("terminal", e.Terminal),
		field("workspace", e.Workspace),
		field("provider", e.Provider),
	}
	lines = append(lines, block("git", e.Git.lines())...)
	lines = append(lines, block("repos", repoLines(e.Repos))...)
	lines = append(lines, block("packages", e.Packages)...)
	lines = append(lines, block("tools", pairLines(e.Tools))...)
	lines = append(lines, block("env", pairLines(e.Env))...)
	lines = append(lines, block("placements", placementLines(e.Placements))...)
	return strings.Join(lines, "\n") + "\n"
}

// field renders one resolved value beside its name, aligned so a reader scans
// the values down a column rather than hunting for them.
func field(name string, value fmt.Stringer) string {
	return fmt.Sprintf("%-11s %s", name+":", value)
}

// block renders a named group of lines indented under its name, and nothing at
// all when the group is empty — an operator reading the resolved configuration
// should see the collections they declared, not a list of the ones they
// did not.
func block(name string, lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, name+":")
	for _, line := range lines {
		out = append(out, "  "+line)
	}
	return out
}

// repoLines renders each repo as the worktree directory it lands in beside the
// remote it clones from, with anything it pins of its own nested underneath.
// The name is the resolved one, which is what makes a defaulted name — and the
// directory two repos would collide in — visible before any box exists.
func repoLines(repos []blueprint.Repo) []string {
	var out []string
	for _, r := range repos {
		out = append(out, fmt.Sprintf("%s (%s)", r.Name, r.URL))
		var nested []string
		if r.Base != "" {
			nested = append(nested, "base: "+r.Base)
		}
		nested = append(nested, block("tools", pairLines(r.Tools))...)
		nested = append(nested, block("env", pairLines(r.Env))...)
		nested = append(nested, block("placements", placementLines(r.Placements))...)
		for _, line := range nested {
			out = append(out, "  "+line)
		}
	}
	return out
}

// pairLines renders a keyed map one entry per line, in key order so two runs
// over the same document read the same way.
func pairLines(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s: %s", k, m[k]))
	}
	return out
}

// placementLines renders each placement as the source it comes from and the
// destination it lands at, with how it is maintained in brackets when the
// operator said. The arrow is the direction the bytes travel.
func placementLines(placements []blueprint.Placement) []string {
	out := make([]string, 0, len(placements))
	for _, p := range placements {
		line := fmt.Sprintf("%s -> %s", p.From, p.To)
		var qualifiers []string
		if p.Mode != "" {
			qualifiers = append(qualifiers, p.Mode)
		}
		if p.Perms != "" {
			qualifiers = append(qualifiers, p.Perms)
		}
		if len(qualifiers) > 0 {
			line += " (" + strings.Join(qualifiers, ", ") + ")"
		}
		out = append(out, line)
	}
	return out
}

// Resolve returns the configuration smith would use, resolved from what the
// operator named on the command line, what the blueprint declares, what their
// preferences declare, and what smith falls back to — in that order of
// precedence, and field by field, so a blueprint pinning one value leaves
// every unrelated preference standing — with the one exception of the
// provider block, which replaces wholesale.
//
// It is pure: three values in, resolved values and their origins out. Nothing
// is read and nothing is printed. A nil blueprint is a run with no blueprint
// named, which is the path preferences alone serve; nil preferences are an
// operator who wrote none.
func Resolve(o Overrides, b *blueprint.Blueprint, p *blueprint.Preferences) Effective {
	if b == nil {
		b = &blueprint.Blueprint{}
	}
	if p == nil {
		p = &blueprint.Preferences{}
	}
	return Effective{
		Access:    resolve(o.Access, b.Access, p.Access, defaultAccess),
		Terminal:  resolve("", b.Terminal, p.Terminal, defaultTerminal),
		Workspace: resolve("", b.Workspace, p.Workspace, defaultWorkspace),
		Provider:  resolveProvider(b.Provider, p.Provider),
		Git: Identity{
			UserName:  resolveDeclared(b.Git.UserName, p.Git.UserName),
			UserEmail: resolveDeclared(b.Git.UserEmail, p.Git.UserEmail),
		},
		Repos:      b.Repos,
		Packages:   b.Packages,
		Tools:      b.Tools,
		Env:        b.Env,
		Placements: b.Placements,
	}
}

// resolveProvider picks one whole adapter, never fields of two. A blueprint
// naming a provider at all replaces the preference block entire; a blueprint
// silent on it inherits the preference entire. This is the single deliberate
// exception to field-level precedence, and it applies to the provider block
// and nothing else.
func resolveProvider(declared, preferred *blueprint.Provider) Adapter {
	switch {
	case declared != nil && preferred != nil:
		return Adapter{Provider: declared, Origin: FromBlueprintReplacing}
	case declared != nil:
		return Adapter{Provider: declared, Origin: FromBlueprint}
	case preferred != nil:
		return Adapter{Provider: preferred, Origin: FromPreferences}
	default:
		return Adapter{}
	}
}

// resolve picks the most specific declaration of one field. An unset value is
// not a declaration, so it falls through to the next one along.
func resolve(flag, declared, preferred, fallback string) Value {
	switch {
	case flag != "":
		return Value{Value: flag, Origin: FromFlag}
	case declared != "":
		return Value{Value: declared, Origin: FromBlueprint}
	case preferred != "":
		return Value{Value: preferred, Origin: FromPreferences}
	default:
		return Value{Value: fallback, Origin: FromDefault}
	}
}

// resolveDeclared picks the most specific declaration of a field smith has no
// default for. A field nobody declared comes back with no value and no origin,
// which is how a caller tells "unset" from "resolved to the empty string".
func resolveDeclared(declared, preferred string) Value {
	switch {
	case declared != "":
		return Value{Value: declared, Origin: FromBlueprint}
	case preferred != "":
		return Value{Value: preferred, Origin: FromPreferences}
	default:
		return Value{}
	}
}
