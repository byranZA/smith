package workspace

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/staging"
)

// Plan derives the ordered units of work from the blueprint and resolution
// staged on the box, with home the smith user's home the box's ~/-relative
// paths resolve against. The blueprint says what the box carries; the
// resolution says where, because its fixed-key fields were resolved on the
// operator's machine and a box resolves nothing itself.
// It is pure: it reads nothing, writes nothing, runs no command and reaches no
// box, so the whole ordering rule can be read and tested without an apt.
//
// A step the blueprint declares nothing for plans nothing, so a blueprint with
// no packages does not report a packages step that had nothing to do. The
// docker step and the orphans scan are the exceptions, and deliberately: it reads what the box holds
// rather than what the blueprint declares, so a blueprint that dropped its last
// repo is exactly the one whose scan must still run.
func Plan(b blueprint.Blueprint, r staging.Resolution, home string) []Unit {
	var plan []Unit
	root := boxPath(r.Workspace, home)
	for _, step := range Order() {
		plan = append(plan, planStep(step, b, r, root, home)...)
	}
	for i := range plan {
		plan[i].Label = labelOf(plan[i])
	}
	for i := range plan {
		plan[i].Needs = prerequisites(plan[i], plan[:i])
	}
	return plan
}

// boxToolchain is the label of the box's own toolchain unit.
const boxToolchain = "toolchain (box)"

// labelOf is the stable name of a unit: its step plus its subject.
func labelOf(u Unit) string {
	switch u.Step {
	case Placements:
		return "placements " + u.Placement.Destination
	case Toolchain:
		if u.Fragment.Repo == "" {
			return boxToolchain
		}
		return "toolchain " + subject(u.Fragment.Repo)
	case Repos:
		return "repos " + u.Repo.Name
	case Identity, Packages, Groups, Docker, Orphans:
		return string(u.Step)
	default:
		return string(u.Step)
	}
}

// subject is a repo name as a toolchain label spells it. A name that would read
// as the box's "(box)" marker, or as an already quoted subject, is quoted, so no
// repo can take the label of the box toolchain and quoting stays injective.
func subject(name string) string {
	if name == "(box)" || strings.HasPrefix(name, `"`) {
		return strconv.Quote(name)
	}
	return name
}

// isBoxToolchain reports whether u is the box's own toolchain unit, by what it
// is rather than by what it is called.
func isBoxToolchain(u Unit) bool {
	return u.Step == Toolchain && u.Fragment.Repo == ""
}

// prerequisites are the labels of the earlier units u cannot be trusted to
// converge without, in plan order. Sibling repos are never each other's, and
// the orphans scan has none.
func prerequisites(u Unit, earlier []Unit) []string {
	var needs []string
	for _, e := range earlier {
		if isPrerequisite(e, u) {
			needs = append(needs, e.Label)
		}
	}
	return needs
}

// isPrerequisite reports whether dependent needs prereq to have converged.
func isPrerequisite(prereq, dependent Unit) bool {
	switch dependent.Step {
	case Repos:
		return prereq.Step == Placements || isBoxToolchain(prereq) ||
			(prereq.Step == Toolchain && prereq.Fragment.Repo == dependent.Repo.Name)
	case Toolchain:
		return dependent.Fragment.Repo != "" && isBoxToolchain(prereq)
	case Groups, Docker:
		return prereq.Step == Packages
	case Placements, Identity, Packages, Orphans:
		return false
	default:
		return false
	}
}

// planStep is what one step of the order plans from the blueprint and the
// resolution, with root the workspace root every repo occupies a directory
// under. The steps
// that are not yet built plan nothing and are named in Order regardless, so the
// slice that builds one adds a case here and inherits its place in the
// sequence rather than deciding it again.
func planStep(step Step, b blueprint.Blueprint, r staging.Resolution, root, home string) []Unit {
	switch {
	case step == Placements:
		return planPlacements(b.Placements, home)
	case step == Identity && r.Git != (staging.Identity{}):
		return []Unit{{Step: Identity, Identity: r.Git, GitConfig: filepath.Join(home, ".gitconfig")}}
	case step == Packages && len(b.Packages) > 0:
		return []Unit{{Step: Packages, Packages: append([]string(nil), b.Packages...)}}
	case step == Groups && len(b.Groups) > 0:
		return []Unit{{Step: Groups, Groups: append([]string(nil), b.Groups...)}}
	case step == Docker:
		return []Unit{{Step: Docker}}
	case step == Repos:
		return planRepos(b, root, home)
	case step == Orphans:
		return []Unit{{Step: Orphans, Workspace: Workspace{
			Root:     root,
			Declared: repoNames(b),
		}}}
	case step == Toolchain:
		return planToolchain(b, root, home)
	default:
		return nil
	}
}

// planToolchain is the box's own toolchain unit and one more for every repo
// that overrides it, in that order: the box-wide versions are pinned before a
// repo narrows them.
//
// Both are one mechanism — write a smith-owned generated file, then run `mise
// install` — differing only in where the file lands and in the directory mise
// resolves it from. A repo that overrides nothing takes the box's toolchain
// and earns no file of its own.
func planToolchain(b blueprint.Blueprint, root, home string) []Unit {
	var units []Unit
	if needsToolchain(b) {
		units = append(units, Unit{Step: Toolchain, Fragment: Fragment{
			Path:  filepath.Join(home, fragmentDir, fragmentFile),
			Mise:  filepath.Join(home, miseDir, miseFile),
			Tools: maps.Clone(b.Tools),
			Env:   exported(b.Env),
		}})
	}
	for _, r := range b.Repos {
		if len(r.Tools) == 0 && len(r.Env) == 0 {
			continue
		}
		dir := filepath.Join(root, cmp.Or(r.Name, blueprint.RepoName(r.URL)))
		units = append(units, Unit{Step: Toolchain, Fragment: Fragment{
			Path:  filepath.Join(dir, repoFragmentFile),
			Dir:   dir,
			Mise:  filepath.Join(home, miseDir, miseFile),
			Repo:  cmp.Or(r.Name, blueprint.RepoName(r.URL)),
			Tools: maps.Clone(r.Tools),
			Env:   exported(r.Env),
		}})
	}
	return units
}

// exported are the names of the variables a fragment exports, sorted, because
// a map has lost the order the operator wrote them in. The values stay behind:
// they were resolved on the operator's machine and staged on the box, and the
// name is what reads one back.
func exported(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(env))
}

// planPlacements is one unit per box-scoped placement the blueprint declares,
// so a placement that cannot be materialized is reported on its own and the
// ones after it are still written.
//
// Only the box scope is planned. A repo's placements are worktree-relative and
// converge when a session stands a worktree up, which is not this stage.
func planPlacements(placements []blueprint.Placement, home string) []Unit {
	units := make([]Unit, 0, len(placements))
	for _, p := range placements {
		units = append(units, Unit{Step: Placements, Placement: Placement{
			Path:        boxPath(p.To, home),
			Destination: p.To,
			Mode:        p.Mode,
			Perms:       p.Perms,
		}})
	}
	return units
}

// planRepos is one unit per declared repo, so a repo that cannot be cloned is
// reported on its own and the ones after it are still converged.
//
// Every repo lands under the resolved workspace root, and a repo the operator
// did not name takes the name its url's last path segment gives it. The repo's own base branch is not read here: it
// is the default a session starts a worktree from, and this stage makes no
// worktree.
func planRepos(b blueprint.Blueprint, root, home string) []Unit {
	mise := filepath.Join(home, miseDir, miseFile)
	units := make([]Unit, 0, len(b.Repos))
	for _, r := range b.Repos {
		name := cmp.Or(r.Name, blueprint.RepoName(r.URL))
		units = append(units, Unit{Step: Repos, Repo: Repo{
			Name: name,
			URL:  r.URL,
			Path: filepath.Join(root, name, cloneDir),
			Dir:  filepath.Join(root, name),
			Mise: mise,
		}})
	}
	return units
}

// repoNames are the names the blueprint's repos occupy directories under the
// workspace root by, each defaulted from its url where the operator named
// none. They are what a directory under the root is held against to decide
// whether it is still declared.
func repoNames(b blueprint.Blueprint) []string {
	names := make([]string, 0, len(b.Repos))
	for _, r := range b.Repos {
		names = append(names, cmp.Or(r.Name, blueprint.RepoName(r.URL)))
	}
	return names
}

// boxPath expands a destination written the way an operator writes it —
// ~/.npmrc — into the absolute path the file lands at. The destination itself
// travels on unexpanded, because it is the key the staged bytes were keyed by.
func boxPath(destination, home string) string {
	if destination == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(destination, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return destination
}

// needsToolchain reports whether the box is owed a toolchain at all. Declared
// tools and env are the obvious halves; a declared repo is the third, because
// a clone runs under mise exec so that the blueprint's env is in scope for it,
// and a box with repos and no mise could not clone one.
func needsToolchain(b blueprint.Blueprint) bool {
	return len(b.Tools) > 0 || len(b.Env) > 0 || len(b.Repos) > 0
}
