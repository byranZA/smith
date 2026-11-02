package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func declaresEverything() blueprint.Blueprint {
	return blueprint.Blueprint{
		Placements: []blueprint.Placement{{From: "file:/op/.netrc", To: "~/.netrc"}},
		Packages:   []string{"ripgrep"},
		Groups:     []string{"docker"},
		Tools:      map[string]string{"node": "22"},
		Repos: []blueprint.Repo{
			{Name: "acme", URL: forge, Tools: map[string]string{"go": "1.24"}},
			{Name: "api", URL: apiURL},
		},
	}
}

func TestPlanGivesEveryUnitItsPrerequisites(t *testing.T) {
	t.Parallel()
	want := map[string][]string{
		"placements ~/.netrc": nil,
		"packages":            nil,
		"groups":              {"packages"},
		"toolchain (box)":     nil,
		"toolchain acme":      {"toolchain (box)"},
		"repos acme":          {"placements ~/.netrc", "toolchain (box)", "toolchain acme"},
		"repos api":           {"placements ~/.netrc", "toolchain (box)"},
		"orphans":             nil,
	}
	got := map[string][]string{}
	for _, u := range Plan(declaresEverything(), resolved, "/home/smith") {
		got[u.Label] = u.Needs
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Plan(declaresEverything()) prerequisites = %v, want %v", got, want)
	}
}

func TestPlanLeavesIdentityWithoutPrerequisites(t *testing.T) {
	t.Parallel()
	r := resolved
	r.Git.UserName = "Ada"
	for _, u := range Plan(blueprint.Blueprint{}, r, "/home/smith") {
		if u.Step == Identity && (u.Label != "identity" || len(u.Needs) != 0) {
			t.Errorf("Plan(empty blueprint) identity unit = label %q needs %v, want label identity and no prerequisites", u.Label, u.Needs)
		}
	}
}

func TestPlanPutsEveryPrerequisiteBeforeItsDependent(t *testing.T) {
	t.Parallel()
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	for i, u := range plan {
		for _, need := range u.Needs {
			at := slices.IndexFunc(plan, func(p Unit) bool { return p.Label == need })
			if at < 0 || at >= i {
				t.Errorf("Plan(declaresEverything()) puts %q at %d, want %q before it, found at %d", u.Label, i, need, at)
			}
		}
	}
}

// failingInstall is a box whose mise install in dir fails.
type failingInstall struct {
	*fakeBox
	dir string
}

func (f failingInstall) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.Contains(strings.Join(untrusted(append([]string{name}, args...)), " "), "--cd "+f.dir+" install") {
		f.calls = append(f.calls, append([]string{name}, args...))
		return errors.New("fetch the pinned versions: no network")
	}
	return f.fakeBox.Run(ctx, name, args, stdin, stdout, stderr)
}

// convergeDeclared converges b through Converge and returns the result and home.
func convergeDeclared(t *testing.T, run Runner, b blueprint.Blueprint, home string, unstaged bool) (Result, string) {
	t.Helper()
	root := t.TempDir()
	stageEnv(t, root, b)
	if !unstaged {
		stage(t, root, "~/.netrc", "machine forge.test\n")
	}
	var progress strings.Builder
	env := Env{Command: run, StateRoot: root, Profile: filepath.Join(t.TempDir(), "smith.sh"), Proc: t.TempDir()}
	plan := Plan(b, resolved, home)
	result, err := Converge(t.Context(), env, plan, &progress)
	if err != nil {
		t.Fatalf("Converge(%d units) error = %v, want nil", len(plan), err)
	}
	return result, progress.String()
}

// readyBox is a box that holds mise and nothing else.
func readyBox() *fakeBox {
	box := newBox()
	box.hasMise = true
	return box
}

// skippedLabels are the labels of the skipped outcomes mapped to their causes.
func skippedLabels(r Result) map[string][]string {
	got := map[string][]string{}
	for _, o := range r.Outcomes {
		if o.Skipped != nil {
			got[o.Label] = o.Skipped
		}
	}
	return got
}

func TestConvergeSkipsEveryRepoOnAFailedPlacement(t *testing.T) {
	t.Parallel()
	box := readyBox()
	result, progress := convergeDeclared(t, box, declaresEverything(), t.TempDir(), true)
	want := map[string][]string{
		"repos acme": {"placements ~/.netrc"},
		"repos api":  {"placements ~/.netrc"},
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
	if !strings.Contains(progress, "  repos       skipped: needs placements ~/.netrc, which failed\n") {
		t.Errorf("Converge() progress = %q, want the skip streamed naming the placement", progress)
	}
	if box.ran("git") {
		t.Errorf("Converge() ran %v on a failed placement, want no git", box.calls)
	}
}

func TestConvergeSkipsRepoUnitsOnAFailedBoxToolchainNamingTheRoot(t *testing.T) {
	t.Parallel()
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), false)
	want := map[string][]string{
		"toolchain acme": {"toolchain (box)"},
		"repos acme":     {"toolchain (box)"},
		"repos api":      {"toolchain (box)"},
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
	if box.ran("git") || box.ran("--cd") {
		t.Errorf("Converge() ran %v on a failed box toolchain, want no clone and no override install", box.calls)
	}
}

func TestConvergeSkipsOnlyTheRepoWhoseOverrideFailed(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	box := readyBox()
	result, _ := convergeDeclared(t, failingInstall{box, filepath.Join(home, "workspace", "acme")}, declaresEverything(), home, false)
	want := map[string][]string{"repos acme": {"toolchain acme"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
	if box.ran(forge) || !box.ran(apiURL) {
		t.Errorf("Converge() ran %v on a failed acme override, want api cloned and acme not", box.calls)
	}
}

func TestConvergeSkipsGroupsOnFailedPackagesButRunsTheRest(t *testing.T) {
	t.Parallel()
	box := readyBox()
	box.aptErr = errors.New("out of disk")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), false)
	want := map[string][]string{"groups": {"packages"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
	if !box.ran(apiURL) {
		t.Errorf("Converge() ran %v on failed packages, want the repos cloned", box.calls)
	}
}

func TestConvergeKeepsSiblingReposConvergingPastAFailure(t *testing.T) {
	t.Parallel()
	box := readyBox()
	box.unreachable[forge] = true
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), false)
	if got := skippedLabels(result); len(got) != 0 {
		t.Errorf("skipped = %v, want none", got)
	}
	if o := outcomesFor(result, Repos); len(o) != 2 || o[0].Err == nil || o[1].Err != nil {
		t.Errorf("repo outcomes = %+v, want acme failed and api converged", o)
	}
}

func TestConvergeStillScansOrphansAfterFailures(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	orphan := filepath.Join(home, "workspace", "gone", "repo.git")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatalf("leave an orphan on the box: %v", err)
	}
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), home, false)
	want := []Outcome{{Step: Orphans, Label: "orphans", Summary: "orphans left in place under " + filepath.Join(home, "workspace") + ": gone"}}
	if got := outcomesFor(result, Orphans); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() after a failed box toolchain orphans outcomes = %+v, want %+v", got, want)
	}
}

func TestConvergeNamesBothIndependentRootFailures(t *testing.T) {
	t.Parallel()
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), true)
	want := []string{"placements ~/.netrc", "toolchain (box)"}
	if got := skippedLabels(result)["repos api"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped causes of repos api = %v, want %v", got, want)
	}
}

func TestReportCountsFailuresAndSkipsAndRestatesBoth(t *testing.T) {
	t.Parallel()
	b := declaresEverything()
	b.Groups = nil
	result, _ := convergeDeclared(t, readyBox(), b, t.TempDir(), true)
	if !result.Failed() {
		t.Error("Result.Failed() after a skip = false, want true")
	}
	got := result.Report()
	for _, want := range []string{
		"1 failed, 2 skipped\n",
		"  placements  failed: ",
		"  repos       skipped: needs placements ~/.netrc, which failed\n  repos       skipped: needs placements ~/.netrc, which failed\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Report() after a failure and skips = %q, want it to contain %q", got, want)
		}
	}
}

func TestSkipAloneLeavesTheRunFailed(t *testing.T) {
	t.Parallel()
	r := Result{Outcomes: []Outcome{{Step: Repos, Skipped: []string{"x"}}}}
	if !r.Failed() {
		t.Error("Result.Failed() with only a skipped outcome = false, want true")
	}
}

func boxNamedRepoBlueprint() blueprint.Blueprint {
	return blueprint.Blueprint{
		Tools: map[string]string{"node": "22"},
		Repos: []blueprint.Repo{
			{Name: "(box)", URL: forge, Tools: map[string]string{"go": "1.24"}},
			{Name: "api", URL: apiURL},
		},
	}
}

// boxNamedOverride is the toolchain unit of the repo named (box).
func boxNamedOverride(t *testing.T, home string) Unit {
	t.Helper()
	for _, u := range Plan(boxNamedRepoBlueprint(), resolved, home) {
		if u.Step == Toolchain && u.Fragment.Repo == "(box)" {
			return u
		}
	}
	t.Fatalf("Plan(boxNamedRepoBlueprint()) has no toolchain unit for the repo (box)")
	return Unit{}
}

func TestConvergeKeepsARepoNamedBoxApartFromTheBoxToolchain(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	override := boxNamedOverride(t, home)
	if override.Label == "toolchain (box)" {
		t.Fatalf("Plan() override label = %q, want one distinct from the box toolchain", override.Label)
	}
	result, _ := convergeDeclared(t, failingInstall{readyBox(), override.Fragment.Dir}, boxNamedRepoBlueprint(), home, false)
	want := map[string][]string{"repos (box)": {override.Label}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
}

func TestConvergeSkipsEverythingOnABoxToolchainFailureBesideARepoNamedBox(t *testing.T) {
	t.Parallel()
	box := newBox()
	box.miseErr = errors.New("no network")
	home := t.TempDir()
	result, _ := convergeDeclared(t, box, boxNamedRepoBlueprint(), home, false)
	root := []string{"toolchain (box)"}
	want := map[string][]string{
		boxNamedOverride(t, home).Label: root,
		"repos (box)":                   root,
		"repos api":                     root,
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("Converge() skipped = %v, want %v", got, want)
	}
}
