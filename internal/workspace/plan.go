package workspace

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
)

// Plan derives the ordered units of work from the blueprint staged on the box,
// with home the smith user's home the box's ~/-relative paths resolve against.
// It is pure: it reads nothing, writes nothing, runs no command and reaches no
// box, so the whole ordering rule can be read and tested without an apt.
//
// A step the blueprint declares nothing for plans nothing, so a blueprint with
// no packages does not report a packages step that had nothing to do.
func Plan(b blueprint.Blueprint, home string) []Unit {
	var plan []Unit
	for _, step := range Order() {
		plan = append(plan, planStep(step, b, home)...)
	}
	return plan
}

// planStep is what one step of the order plans from the blueprint. The steps
// that are not yet built plan nothing and are named in Order regardless, so the
// slice that builds one adds a case here and inherits its place in the
// sequence rather than deciding it again.
func planStep(step Step, b blueprint.Blueprint, home string) []Unit {
	switch {
	case step == Placements:
		return planPlacements(b.Placements, home)
	case step == Packages && len(b.Packages) > 0:
		return []Unit{{Step: Packages, Packages: append([]string(nil), b.Packages...)}}
	case step == Repos:
		return planRepos(b, home)
	case step == Orphans && len(b.Repos) > 0:
		return []Unit{{Step: Orphans, Workspace: Workspace{
			Root:     workspaceRoot(b, home),
			Declared: repoNames(b),
		}}}
	case step == Toolchain:
		return planToolchain(b, home)
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
func planToolchain(b blueprint.Blueprint, home string) []Unit {
	var units []Unit
	if needsToolchain(b) {
		units = append(units, Unit{Step: Toolchain, Fragment: Fragment{
			Path:  filepath.Join(home, fragmentDir, fragmentFile),
			Mise:  filepath.Join(home, miseDir, miseFile),
			Tools: maps.Clone(b.Tools),
			Env:   exported(b.Env),
		}})
	}
	root := workspaceRoot(b, home)
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

// workspaceRoot is the directory every repo occupies a directory under: the
// blueprint's when it declares one, and ~/workspace otherwise.
func workspaceRoot(b blueprint.Blueprint, home string) string {
	return boxPath(cmp.Or(b.Workspace, defaultWorkspace), home)
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
// The workspace root is the blueprint's when it declares one and ~/workspace
// otherwise, and a repo the operator did not name takes the name its url's
// last path segment gives it. The repo's own base branch is not read here: it
// is the default a session starts a worktree from, and this stage makes no
// worktree.
func planRepos(b blueprint.Blueprint, home string) []Unit {
	root := workspaceRoot(b, home)
	mise := filepath.Join(home, miseDir, miseFile)
	units := make([]Unit, 0, len(b.Repos))
	for _, r := range b.Repos {
		name := cmp.Or(r.Name, blueprint.RepoName(r.URL))
		units = append(units, Unit{Step: Repos, Repo: Repo{
			Name: name,
			URL:  r.URL,
			Path: filepath.Join(root, name, cloneDir),
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
