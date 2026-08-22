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
}

// String renders the resolved configuration as one line per field, aligned, in
// schema order.
func (e Effective) String() string {
	fields := []struct {
		name  string
		value Value
	}{
		{"access", e.Access},
		{"terminal", e.Terminal},
		{"workspace", e.Workspace},
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
// every unrelated preference standing.
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
