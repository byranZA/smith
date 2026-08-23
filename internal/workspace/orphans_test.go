package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// TestConvergeReportsADroppedRepoAsAnOrphan proves a repo the operator removed
// from the blueprint is named back to them and left exactly where it is. The
// directory holds a bare repo, its worktrees, and possibly work that is not
// pushed anywhere, so smith reports it and destroys nothing.
func TestConvergeReportsADroppedRepoAsAnOrphan(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	dropped := clonePath(home, "api")
	alreadyCloned(t, box, dropped, "https://forge.test/api.git")

	result, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want an orphan reported rather than a failure: %s", result.Report())
	}
	if !strings.Contains(progress, "orphan") || !strings.Contains(progress, "api") {
		t.Errorf("progress = %q, want the dropped repo reported as an orphan", progress)
	}
	if _, err := os.Stat(dropped); err != nil {
		t.Errorf("the dropped repo is gone after the stage ran: %v", err)
	}
}

// TestConvergeReportsNoOrphanForADeclaredRepo proves the repos the blueprint
// still declares are not reported as orphans of themselves.
func TestConvergeReportsNoOrphanForADeclaredRepo(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	alreadyCloned(t, box, clonePath(home, "acme"), forge)

	_, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	if strings.Contains(progress, "orphan") && !strings.Contains(progress, "no orphans") {
		t.Errorf("progress = %q, want a declared repo not reported as an orphan", progress)
	}
}

// TestConvergeLeavesADirectoryThatIsNotARepoAlone proves a directory the
// operator put under the workspace root themselves is neither an orphan nor
// smith's business: only a directory holding a bare clone is a repo.
func TestConvergeLeavesADirectoryThatIsNotARepoAlone(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	notes := filepath.Join(home, "workspace", "notes")
	if err := os.MkdirAll(notes, 0o700); err != nil {
		t.Fatalf("put a directory of the operator's own at %s: %v", notes, err)
	}

	_, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	if strings.Contains(progress, "notes") {
		t.Errorf("progress = %q, want a directory that holds no clone left unreported", progress)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("the operator's own directory is gone after the stage ran: %v", err)
	}
}

// TestConvergeReportsEveryOrphan proves the operator is told about all of the
// repos the blueprint dropped, not the first one alphabetically.
func TestConvergeReportsEveryOrphan(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	alreadyCloned(t, box, clonePath(home, "api"), "https://forge.test/api.git")
	alreadyCloned(t, box, clonePath(home, "legacy"), "https://forge.test/legacy.git")

	_, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	for _, name := range []string{"api", "legacy"} {
		if !strings.Contains(progress, name) {
			t.Errorf("progress = %q, want %s reported as an orphan", progress, name)
		}
	}
}

// TestPlanScansForOrphansUnderTheWorkspaceRoot proves the scan is planned
// against the root the repos land under, and that a blueprint declaring no
// repos still plans one — the scan reads the box, so dropping every repo
// leaves every clone the box holds an orphan to report.
func TestPlanScansForOrphansUnderTheWorkspaceRoot(t *testing.T) {
	b := blueprint.Blueprint{Workspace: "~/code", Repos: []blueprint.Repo{{URL: forge}}}

	units := unitsFor(Plan(b, "/home/smith"), Orphans)

	if len(units) != 1 {
		t.Fatalf("Plan() planned %d orphans units, want 1", len(units))
	}
	if got := units[0].Workspace.Root; got != "/home/smith/code" {
		t.Errorf("orphans unit root = %q, want the declared workspace root", got)
	}
	if got := strings.Join(units[0].Workspace.Declared, ","); got != "acme" {
		t.Errorf("orphans unit declares %q, want the repo's defaulted name", got)
	}
	planned := unitsFor(Plan(blueprint.Blueprint{Packages: []string{"jq"}}, "/home/smith"), Orphans)
	if len(planned) != 1 {
		t.Fatalf("a blueprint declaring no repos planned %d orphans units, want 1", len(planned))
	}
	if got := planned[0].Workspace.Root; got != "/home/smith/workspace" {
		t.Errorf("orphans unit root = %q, want the default workspace root", got)
	}
	if got := len(planned[0].Workspace.Declared); got != 0 {
		t.Errorf("orphans unit declares %d repos, want none", got)
	}
}

// TestConvergeReportsAnOrphanWhenTheBlueprintDeclaresNoRepo proves that
// dropping the last repo still reports the clone the box holds. The scan reads
// what the box has, not what the blueprint declares, so a blueprint that
// removes every repo is the case that most needs the report rather than the one
// that turns it off.
func TestConvergeReportsAnOrphanWhenTheBlueprintDeclaresNoRepo(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	dropped := clonePath(home, "api")
	alreadyCloned(t, box, dropped, apiURL)
	notes := filepath.Join(home, "workspace", "notes")
	if err := os.MkdirAll(notes, 0o700); err != nil {
		t.Fatalf("put a directory of the operator's own at %s: %v", notes, err)
	}

	result, progress := convergeOnBox(t, box, home, blueprint.Blueprint{})

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want an orphan reported rather than a failure: %s", result.Report())
	}
	if !strings.Contains(progress, "orphan") || !strings.Contains(progress, "api") {
		t.Errorf("progress = %q, want the dropped repo reported as an orphan", progress)
	}
	if strings.Contains(progress, "notes") {
		t.Errorf("progress = %q, want a directory that holds no clone left unreported", progress)
	}
	for _, path := range []string{dropped, notes} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s is gone after the stage ran: %v", path, err)
		}
	}
}

// unitsFor narrows a plan to the units of one step, which is how a test asks
// what a step planned without depending on where it falls in the order.
func unitsFor(plan []Unit, step Step) []Unit {
	var units []Unit
	for _, unit := range plan {
		if unit.Step == step {
			units = append(units, unit)
		}
	}
	return units
}
