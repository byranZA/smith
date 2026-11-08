package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/staging"
)

// checkDocument is the JSON document `blueprint check --json` prints.
type checkDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	Preferences   checkFile             `json:"preferences"`
	Blueprint     *checkFile            `json:"blueprint,omitempty"`
	Resolved      *checkResolved        `json:"resolved,omitempty"`
	References    *checkReferenceReport `json:"references,omitempty"`
	Problems      []checkProblem        `json:"problems"`
}

// checkReferenceReport is whether `smith machine setup` from here would resolve everything the blueprint declares.
type checkReferenceReport struct {
	Resolve       bool              `json:"resolve"`
	Unresolved    []checkUnresolved `json:"unresolved,omitempty"`
	EmptyLiterals []checkUnresolved `json:"empty_literals,omitempty"`
}

// checkUnresolved is one declaration that would not resolve, by scope and reason but never by value.
type checkUnresolved struct {
	Scope  string `json:"scope"`
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// newCheckUnresolved shapes what did not resolve as the document's entries.
func newCheckUnresolved(u *staging.UnresolvedError) []checkUnresolved {
	var out []checkUnresolved
	for _, s := range u.Sources {
		out = append(out, checkUnresolved{Scope: s.Scope(), Ref: s.From, Reason: s.Err.Error()})
	}
	for _, v := range u.Values {
		out = append(out, checkUnresolved{Scope: v.Scope(), Ref: v.Ref, Reason: v.Err.Error()})
	}
	return out
}

// checkFile names a config file the check read. Found is set for preferences, whose file is optional.
type checkFile struct {
	Name  string `json:"name,omitempty"`
	Path  string `json:"path"`
	Found *bool  `json:"found,omitempty"`
}

// checkProblem is one finding against a config document.
type checkProblem struct {
	Line    int    `json:"line,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// checkValue is a resolved field beside the token naming where it came from.
type checkValue struct {
	Value  string        `json:"value"`
	Origin config.Origin `json:"origin"`
}

// checkProvider is the resolved adapter, which replaces the preference's wholesale.
type checkProvider struct {
	checkValue
	ReplacesPreference bool `json:"replaces_preference,omitempty"`
}

// checkGit holds the halves of the git identity somebody declared.
type checkGit struct {
	UserName  *checkValue `json:"user_name,omitempty"`
	UserEmail *checkValue `json:"user_email,omitempty"`
}

// checkRepo is one repo the box carries, each value beside its origin.
type checkRepo struct {
	Name       checkValue            `json:"name"`
	URL        checkValue            `json:"url"`
	Base       *checkValue           `json:"base,omitempty"`
	Tools      map[string]checkValue `json:"tools,omitempty"`
	Env        map[string]checkValue `json:"env,omitempty"`
	Placements []checkPlacement      `json:"placements,omitempty"`
}

// checkPlacement is one file put on the box, from its source to its destination.
type checkPlacement struct {
	From   string        `json:"from"`
	To     string        `json:"to"`
	Mode   string        `json:"mode,omitempty"`
	Perms  string        `json:"perms,omitempty"`
	Origin config.Origin `json:"origin"`
}

// The collections come only from the blueprint, so every entry of them carries that origin.

// newCheckPlacements shapes placements as the document's entries.
func newCheckPlacements(placements []blueprint.Placement) []checkPlacement {
	var out []checkPlacement
	for _, p := range placements {
		out = append(out, checkPlacement{From: p.From, To: p.To, Mode: p.Mode, Perms: p.Perms, Origin: config.FromBlueprint})
	}
	return out
}

// newCheckValues shapes each entry of a list as a value beside its origin.
func newCheckValues(items []string) []checkValue {
	var out []checkValue
	for _, item := range items {
		out = append(out, checkValue{Value: item, Origin: config.FromBlueprint})
	}
	return out
}

// newCheckPairs shapes each entry of a keyed map as a value beside its origin.
func newCheckPairs(m map[string]string) map[string]checkValue {
	var out map[string]checkValue
	for k, v := range m {
		if out == nil {
			out = map[string]checkValue{}
		}
		out[k] = checkValue{Value: v, Origin: config.FromBlueprint}
	}
	return out
}

// checkResolved is the configuration smith would use.
type checkResolved struct {
	Access     checkValue            `json:"access"`
	Terminal   checkValue            `json:"terminal"`
	Workspace  checkValue            `json:"workspace"`
	Provider   *checkProvider        `json:"provider,omitempty"`
	Git        *checkGit             `json:"git,omitempty"`
	Repos      []checkRepo           `json:"repos,omitempty"`
	Packages   []checkValue          `json:"packages,omitempty"`
	Groups     []checkValue          `json:"groups,omitempty"`
	Tools      map[string]checkValue `json:"tools,omitempty"`
	Env        map[string]checkValue `json:"env,omitempty"`
	Placements []checkPlacement      `json:"placements,omitempty"`
}

// newCheckResolved shapes the resolved configuration as the document's values.
func newCheckResolved(r config.Resolved) *checkResolved {
	out := &checkResolved{
		Access:     checkValue(r.Access),
		Terminal:   checkValue(r.Terminal),
		Workspace:  checkValue(r.Workspace),
		Packages:   newCheckValues(r.Packages),
		Groups:     newCheckValues(r.Groups),
		Tools:      newCheckPairs(r.Tools),
		Env:        newCheckPairs(r.Env),
		Placements: newCheckPlacements(r.Placements),
	}
	if r.Provider.Provider != nil {
		out.Provider = &checkProvider{
			checkValue:         checkValue{Value: adapterCommand(r.Provider), Origin: r.Provider.Origin},
			ReplacesPreference: r.Provider.Replaces,
		}
	}
	if half := declaredValue(r.Git.UserName); half != nil || declaredValue(r.Git.UserEmail) != nil {
		out.Git = &checkGit{UserName: half, UserEmail: declaredValue(r.Git.UserEmail)}
	}
	for _, repo := range r.Repos {
		entry := checkRepo{
			Name:  checkValue{Value: repo.Name, Origin: config.FromBlueprint},
			URL:   checkValue{Value: repo.URL, Origin: config.FromBlueprint},
			Tools: newCheckPairs(repo.Tools), Env: newCheckPairs(repo.Env),
			Placements: newCheckPlacements(repo.Placements),
		}
		if repo.Base != "" {
			entry.Base = &checkValue{Value: repo.Base, Origin: config.FromBlueprint}
		}
		out.Repos = append(out.Repos, entry)
	}
	return out
}

// declaredValue is v as a document value, or nil when nobody declared it.
func declaredValue(v config.Value) *checkValue {
	if v.Origin == "" {
		return nil
	}
	c := checkValue(v)
	return &c
}

// adapterCommand is the provider CLI the adapter drives, or "declared" when it has no create template.
func adapterCommand(a config.Adapter) string {
	if len(a.Provider.Create) == 0 {
		return "declared"
	}
	return a.Provider.Create[0]
}

// problemsOf lists the findings err carries, and reports whether it carried any. A malformed document has its one finding.
func problemsOf(err error) ([]checkProblem, bool) {
	var invalid *blueprint.ValidationError
	if !errors.As(err, &invalid) {
		return nil, false
	}
	problems := make([]checkProblem, 0, len(invalid.Findings))
	for _, f := range invalid.Findings {
		problems = append(problems, checkProblem{Line: f.Line, Path: f.Path, Message: f.Message})
	}
	return problems, true
}

// newCheckDocument shapes a check result as the JSON document. A document with problems is still an answer.
func newCheckDocument(r checkResult) checkDocument {
	found := r.Prefs.Found
	doc := checkDocument{SchemaVersion: 1, Problems: []checkProblem{}, Preferences: checkFile{Path: r.Prefs.Path, Found: &found}}
	if r.HasDoc {
		doc.Blueprint = &checkFile{Name: r.Name, Path: r.Doc.Path}
	}
	if r.Findings != nil {
		doc.Problems, _ = problemsOf(r.Findings)
		return doc
	}
	doc.Resolved = newCheckResolved(*r.Resolved)
	if r.References != nil {
		doc.References = &checkReferenceReport{
			Resolve:       r.References.Count()+r.Literals.Count() == 0,
			Unresolved:    newCheckUnresolved(r.References),
			EmptyLiterals: newCheckUnresolved(r.Literals),
		}
	}
	return doc
}

// printCheck writes doc, and carries exit code 1 out when it holds problems.
func printCheck(cmd *cobra.Command, doc checkDocument) error {
	if err := writeDocument(cmd.OutOrStdout(), doc); err != nil {
		return err
	}
	if len(doc.Problems) > 0 {
		return &exitError{code: 1}
	}
	return nil
}
