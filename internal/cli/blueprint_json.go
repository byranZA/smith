package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
)

// checkDocument is the JSON document `blueprint check --json` prints.
type checkDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Preferences   checkFile      `json:"preferences"`
	Blueprint     *checkFile     `json:"blueprint,omitempty"`
	Resolved      *checkResolved `json:"resolved,omitempty"`
	Problems      []checkProblem `json:"problems"`
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

// checkRepo is one repo the box carries.
type checkRepo struct {
	Name       string            `json:"name"`
	URL        string            `json:"url"`
	Base       string            `json:"base,omitempty"`
	Tools      map[string]string `json:"tools,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Placements []checkPlacement  `json:"placements,omitempty"`
}

// checkPlacement is one file put on the box, from its source to its destination.
type checkPlacement struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Mode  string `json:"mode,omitempty"`
	Perms string `json:"perms,omitempty"`
}

// newCheckPlacements shapes placements as the document's entries.
func newCheckPlacements(placements []blueprint.Placement) []checkPlacement {
	var out []checkPlacement
	for _, p := range placements {
		out = append(out, checkPlacement{From: p.From, To: p.To, Mode: p.Mode, Perms: p.Perms})
	}
	return out
}

// checkResolved is the configuration smith would use.
type checkResolved struct {
	Access     checkValue        `json:"access"`
	Terminal   checkValue        `json:"terminal"`
	Workspace  checkValue        `json:"workspace"`
	Provider   *checkProvider    `json:"provider,omitempty"`
	Git        *checkGit         `json:"git,omitempty"`
	Repos      []checkRepo       `json:"repos,omitempty"`
	Packages   []string          `json:"packages,omitempty"`
	Groups     []string          `json:"groups,omitempty"`
	Tools      map[string]string `json:"tools,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Placements []checkPlacement  `json:"placements,omitempty"`
}

// newCheckResolved shapes the resolved configuration as the document's values.
func newCheckResolved(r config.Resolved) *checkResolved {
	out := &checkResolved{
		Access:     checkValue(r.Access),
		Terminal:   checkValue(r.Terminal),
		Workspace:  checkValue(r.Workspace),
		Packages:   r.Packages,
		Groups:     r.Groups,
		Tools:      r.Tools,
		Env:        r.Env,
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
		out.Repos = append(out.Repos, checkRepo{
			Name: repo.Name, URL: repo.URL, Base: repo.Base,
			Tools: repo.Tools, Env: repo.Env, Placements: newCheckPlacements(repo.Placements),
		})
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

// runCheckJSON is `blueprint check --json`. A document with problems is still an answer, so it prints and exits 1; a blueprint that cannot be found or read is not, so it prints nothing on stdout.
func runCheckJSON(cmd *cobra.Command, home config.Home, args []string, accessMode string) error {
	prefs, err := config.LoadPreferences(home)
	doc := checkDocument{SchemaVersion: 1, Problems: []checkProblem{}}
	found := prefs.Found
	doc.Preferences = checkFile{Path: prefs.Path, Found: &found}
	if err != nil {
		problems, ok := problemsOf(err)
		if !ok {
			return reportInvalid(cmd, err)
		}
		found = true
		doc.Problems = problems
		return printCheck(cmd, doc)
	}
	overrides := config.Overrides{Access: accessMode}
	if len(args) == 0 {
		doc.Resolved = newCheckResolved(config.Resolve(overrides, nil, &prefs.Declared))
		return printCheck(cmd, doc)
	}
	loaded, err := config.LoadDocument(home, args[0])
	if err != nil {
		problems, ok := problemsOf(err)
		if !ok {
			return reportInvalid(cmd, err)
		}
		doc.Blueprint = &checkFile{Name: args[0], Path: loaded.Path}
		doc.Problems = problems
		return printCheck(cmd, doc)
	}
	doc.Blueprint = &checkFile{Name: args[0], Path: loaded.Path}
	resolved := config.Resolve(overrides, &loaded.Blueprint, &prefs.Declared)
	if err := resolved.Conflicts(); err != nil {
		problems, _ := problemsOf(err)
		doc.Problems = problems
		return printCheck(cmd, doc)
	}
	doc.Resolved = newCheckResolved(resolved)
	return printCheck(cmd, doc)
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
