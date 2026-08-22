package blueprint

import (
	"errors"
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

// gitconfigPath is the file git reads a box-wide identity from. Git reads it
// or ~/.config/git/config and never both, so this is the one path the
// identity fields and a box placement can collide on.
const gitconfigPath = "~/.gitconfig"

// placementScope is what a placement's destination is anchored to, which
// follows from where the placement is declared and nothing else: at the top
// level a placement is box-scoped, under a repos[] entry it is repo-scoped.
// The two read as the same shape at different indents, and the repos: header
// that settles the question can be far above the line being read, so the
// destination is made to state the scope it was declared in.
type placementScope int

const (
	// boxScope anchors a destination to the box, so it is an absolute or
	// ~-relative path.
	boxScope placementScope = iota
	// repoScope anchors a destination to the repo's worktree, so it is a
	// relative path.
	repoScope
)

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
	report = append(report, gitFindings(b.Git, b.Placements)...)
	report = append(report, envFindings("env", b.Env)...)
	report = append(report, placementFindings("placements", b.Placements, boxScope)...)
	report = append(report, repoFindings(b.Repos)...)
	return report
}

// gitFindings refuses a blueprint that names two writers for the same
// ~/.gitconfig. Identity fields mean smith writes that file itself, so a box
// placement to it would land on top of what smith wrote, in whichever order
// the two happened to run. Only the operator knows which they meant, so the
// pair is refused rather than resolved.
func gitFindings(identity Git, placements []Placement) []Finding {
	if identity == (Git{}) {
		return nil
	}
	for _, p := range placements {
		if p.To != gitconfigPath {
			continue
		}
		return []Finding{{Path: "git", Message: fmt.Sprintf(
			"git identity fields and a box placement to %q are mutually exclusive, because both write that file: choose which one does, and drop the other",
			gitconfigPath)}}
	}
	return nil
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
		report = append(report, envFindings(at+".env", r.Env)...)
		report = append(report, placementFindings(at+".placements", r.Placements, repoScope)...)
	}
	return report
}

// placementFindings checks the values a placement constrains, under the path
// the placements were declared at and in the scope that path puts them in.
func placementFindings(path string, placements []Placement, scope placementScope) []Finding {
	var report []Finding
	for i, p := range placements {
		at := fmt.Sprintf("%s[%d]", path, i)
		report = append(report, source(at+".from", p.From)...)
		report = append(report, destination(at+".to", p.To, scope)...)
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

// destination refuses a placement destination that disagrees with the scope
// it was declared in — a box placement that names no anchor, or a repo
// placement that names one outside the worktree. The repo case names box
// scope as the alternative, because writing outside the worktree is what the
// operator meant and declaring it one level up is how they get it.
func destination(path, to string, scope placementScope) []Finding {
	if to == "" {
		return nil
	}
	anchored := strings.HasPrefix(to, "/") || strings.HasPrefix(to, "~")
	switch {
	case scope == boxScope && !anchored:
		return []Finding{{Path: path, Message: fmt.Sprintf(
			"%q is relative, but a box placement is anchored to the box: write an absolute path, or one starting with %q",
			to, "~/")}}
	case scope == repoScope && anchored:
		return []Finding{{Path: path, Message: fmt.Sprintf(
			"%q leaves the worktree, but a repo placement is worktree-relative: to write it outside the worktree, declare it at box scope in the top-level %q instead",
			to, "placements")}}
	default:
		return nil
	}
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

// ValidateAccess refuses an access mode smith does not recognise, so a caller
// taking one from somewhere outside a config document — a command-line flag,
// which outranks every declared value — refuses it in the same words and
// against the same set as a blueprint declaring it.
func ValidateAccess(mode string) error {
	report := choice("access", mode, accessModes)
	if len(report) == 0 {
		return nil
	}
	return errors.New(report[0].Message)
}
