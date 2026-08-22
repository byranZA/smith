package blueprint

// Preferences is the operator's own half of the config home: what is a
// property of *them* rather than of a kind of box. Its fields answer "if you
// built a completely different box tomorrow, should this value follow?" with
// yes.
//
// It carries no collection fields, and that omission is the whole of the rule
// that keeps repos, placements, packages, tools and env blueprint-only: a
// collection written into a preferences file is refused by the strict pass as
// a field the schema does not define, so there is no allowlist to write and
// none to maintain. Do not add a validation rule that duplicates it.
//
// The reason for the split is reproducibility, not merge semantics. A box's
// marker records which blueprint it was built from and records nothing about
// preferences, so the blueprint pointer has to be enough to reproduce the box.
// If repos could come from preferences, a teammate who clones ~/.smith and
// runs `machine setup prod` could get a different set of repos while both
// boxes truthfully record `blueprint: prod` — the pointer would be a lie. The
// merge ambiguity (does a blueprint's list replace a preference's, or append
// to it?) is a symptom of the same thing.
type Preferences struct {
	// Access is how boxes are reached: "public" or "tailscale".
	Access string `yaml:"access"`
	// Terminal is the terminal substrate sessions run under.
	Terminal string `yaml:"terminal"`
	// Workspace is the directory on a box the repo worktrees live under.
	Workspace string `yaml:"workspace"`
	// Git is the identity boxes commit as.
	Git Git `yaml:"git"`
	// Provider is the adapter smith creates and lists boxes through. A
	// blueprint declaring a provider replaces this one wholesale rather than
	// field by field, which is why it is a pointer: absent and empty differ.
	Provider *Provider `yaml:"provider"`
}

// ParsePreferences decodes a preferences document, refusing any key the schema
// does not define — including every blueprint-only collection — and any value
// smith would not know what to do with. An empty document is valid
// preferences with every field unset.
//
// As with a blueprint, every problem is reported in one pass: the returned
// error is a *ValidationError carrying an ordered list of findings, and a
// document that is not YAML at all is reported as malformed instead.
func ParsePreferences(data []byte) (Preferences, error) {
	var p Preferences
	_, findings, err := decodeStrict(data, preferencesSubject, &p)
	if err != nil {
		return Preferences{}, err
	}
	findings = append(findings, choice("access", p.Access, accessModes)...)
	findings = append(findings, choice("terminal", p.Terminal, terminals)...)
	if len(findings) > 0 {
		return Preferences{}, &ValidationError{Subject: preferencesSubject, Findings: findings}
	}
	return p, nil
}
