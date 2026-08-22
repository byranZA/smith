package blueprint

import (
	"fmt"
	"regexp"
	"strings"
)

// The values smith recognises for each constrained field. A value outside
// these sets parses cleanly and means nothing, which is exactly the silent
// success the blueprint surface exists to prevent.
var (
	accessModes    = []string{"public", "tailscale"}
	terminals      = []string{"tmux"}
	placementModes = []string{"converge", "once"}
	// octalPerms is a file mode written the way chmod takes it.
	octalPerms = regexp.MustCompile(`^[0-7]{3,4}$`)
)

// gitSuffix is the extension a clone URL carries that a worktree directory
// does not.
const gitSuffix = ".git"

// withDefaults fills in what the operator left implicit, so every later reader
// sees one shape. Returning a new blueprint rather than editing one in place
// keeps the defaults observable in a test.
func withDefaults(b Blueprint) Blueprint {
	if len(b.Repos) == 0 {
		return b
	}
	repos := make([]Repo, len(b.Repos))
	copy(repos, b.Repos)
	for i := range repos {
		if repos[i].Name == "" {
			repos[i].Name = repoName(repos[i].URL)
		}
	}
	b.Repos = repos
	return b
}

// repoName is the worktree directory a clone URL lands in: its last path
// segment without the .git a remote carries. Forges spell the separator
// before the path either way, so both are cut.
func repoName(url string) string {
	segment := url
	if i := strings.LastIndexAny(segment, "/:"); i >= 0 {
		segment = segment[i+1:]
	}
	return strings.TrimSuffix(segment, gitSuffix)
}

// validate applies the rules a strict structural pass is blind to, walking the
// document in the order it is written so the report reads top to bottom.
func validate(b Blueprint) []Finding {
	var report []Finding
	report = append(report, choice("access", b.Access, accessModes)...)
	report = append(report, choice("terminal", b.Terminal, terminals)...)
	report = append(report, placementFindings("placements", b.Placements)...)
	report = append(report, repoFindings(b.Repos)...)
	return report
}

// repoFindings checks each repo carries a remote and that no two of them would
// land in the same workspace directory.
func repoFindings(repos []Repo) []Finding {
	var report []Finding
	claimed := map[string]bool{}
	for i, r := range repos {
		at := fmt.Sprintf("repos[%d]", i)
		if r.URL == "" {
			report = append(report, Finding{Path: at, Message: "a repo needs a url naming the remote to clone"})
		}
		switch {
		case r.Name == "":
		case claimed[r.Name]:
			report = append(report, Finding{
				Path:    at,
				Message: fmt.Sprintf("two repos resolve to the name %q, and would collide at the same workspace directory", r.Name),
			})
		default:
			claimed[r.Name] = true
		}
		report = append(report, placementFindings(at+".placements", r.Placements)...)
	}
	return report
}

// placementFindings checks the values a placement constrains, under the path
// the placements were declared at.
func placementFindings(path string, placements []Placement) []Finding {
	var report []Finding
	for i, p := range placements {
		at := fmt.Sprintf("%s[%d]", path, i)
		report = append(report, choice(at+".mode", p.Mode, placementModes)...)
		if p.Perms != "" && !octalPerms.MatchString(p.Perms) {
			report = append(report, Finding{
				Path:    at + ".perms",
				Message: fmt.Sprintf("permissions %q are not a file mode: write it in octal, such as %q", p.Perms, "0600"),
			})
		}
	}
	return report
}

// choice refuses a value outside the set smith recognises for a field. An
// unset field is not a choice the operator made, so it passes and the
// precedence chain fills it in later.
func choice(path, value string, known []string) []Finding {
	if value == "" {
		return nil
	}
	for _, k := range known {
		if value == k {
			return nil
		}
	}
	return []Finding{{Path: path, Message: fmt.Sprintf("%q is not recognised here: smith knows %s", value, list(known))}}
}

// list renders the known values of a field as prose, so a single-valued field
// reads as the statement it is rather than as a list of one.
func list(known []string) string {
	quoted := make([]string, len(known))
	for i, k := range known {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	switch len(quoted) {
	case 1:
		return quoted[0] + " and nothing else in v1"
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}
