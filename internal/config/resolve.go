package config

import (
	"fmt"
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
}

// String renders the resolved configuration as one line per field, aligned, in
// schema order.
func (e Effective) String() string {
	fields := []struct {
		name  string
		value fmt.Stringer
	}{
		{"access", e.Access},
		{"terminal", e.Terminal},
		{"workspace", e.Workspace},
		{"provider", e.Provider},
	}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(fmt.Sprintf("%-10s %s\n", f.name+":", f.value))
	}
	return b.String()
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
