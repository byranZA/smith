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

// declaresEverything is a blueprint with every kind of unit, and a repo that
// overrides the box toolchain beside one that does not.
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
		t.Errorf("Plan() prerequisites = %v, want %v", got, want)
	}
}

func TestPlanLeavesIdentityWithoutPrerequisites(t *testing.T) {
	r := resolved
	r.Git.UserName = "Ada"
	for _, u := range Plan(blueprint.Blueprint{}, r, "/home/smith") {
		if u.Step == Identity && (u.Label != "identity" || len(u.Needs) != 0) {
			t.Errorf("identity unit = label %q needs %v, want label identity and no prerequisites", u.Label, u.Needs)
		}
	}
}

func TestPlanPutsEveryPrerequisiteBeforeItsDependent(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	for i, u := range plan {
		for _, need := range u.Needs {
			at := slices.IndexFunc(plan, func(p Unit) bool { return p.Label == need })
			if at < 0 || at >= i {
				t.Errorf("%q needs %q, which sits at %d, not before %d", u.Label, need, at, i)
			}
		}
	}
}

// failingInstall is a box whose mise install in one directory fails, the way a
// toolchain that cannot be fetched does, while every other command is the box's.
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

// convergeDeclared converges what a blueprint declares through Converge, with
// the netrc placement staged unless left unstaged, and returns the home it ran
// against.
func convergeDeclared(t *testing.T, run Runner, b blueprint.Blueprint, home string, unstaged bool) (Result, string) {
	t.Helper()
	root := t.TempDir()
	stageEnv(t, root, b)
	if !unstaged {
		stage(t, root, "~/.netrc", "machine forge.test\n")
	}
	var progress strings.Builder
	env := Env{Command: run, StateRoot: root, Profile: filepath.Join(t.TempDir(), "smith.sh"), Proc: t.TempDir()}
	result, err := Converge(context.Background(), env, Plan(b, resolved, home), &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
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
	box := readyBox()
	result, progress := convergeDeclared(t, box, declaresEverything(), t.TempDir(), true)
	want := map[string][]string{
		"repos acme": {"placements ~/.netrc"},
		"repos api":  {"placements ~/.netrc"},
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
	if !strings.Contains(progress, "  repos       skipped: needs placements ~/.netrc, which failed\n") {
		t.Errorf("progress = %q, want the skip streamed naming the placement", progress)
	}
	if box.ran("git") {
		t.Errorf("the run ran %v, want no git for repos skipped on a failed placement", box.calls)
	}
}

func TestConvergeSkipsRepoUnitsOnAFailedBoxToolchainNamingTheRoot(t *testing.T) {
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), false)
	want := map[string][]string{
		"toolchain acme": {"toolchain (box)"},
		"repos acme":     {"toolchain (box)"},
		"repos api":      {"toolchain (box)"},
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
	if box.ran("git") || box.ran("--cd") {
		t.Errorf("the run ran %v, want no clone and no override install", box.calls)
	}
}

func TestConvergeSkipsOnlyTheRepoWhoseOverrideFailed(t *testing.T) {
	home := t.TempDir()
	box := readyBox()
	result, _ := convergeDeclared(t, failingInstall{box, filepath.Join(home, "workspace", "acme")}, declaresEverything(), home, false)
	want := map[string][]string{"repos acme": {"toolchain acme"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
	if box.ran(forge) || !box.ran(apiURL) {
		t.Errorf("the run ran %v, want api cloned and acme not", box.calls)
	}
}

func TestConvergeSkipsGroupsOnFailedPackagesButRunsTheRest(t *testing.T) {
	box := readyBox()
	box.aptErr = errors.New("out of disk")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), false)
	want := map[string][]string{"groups": {"packages"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
	if !box.ran(apiURL) {
		t.Errorf("the run ran %v, want the repos cloned despite the packages failure", box.calls)
	}
}

func TestConvergeKeepsSiblingReposConvergingPastAFailure(t *testing.T) {
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
	home := t.TempDir()
	orphan := filepath.Join(home, "workspace", "gone", "repo.git")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatalf("leave an orphan on the box: %v", err)
	}
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), home, false)
	o := outcomesFor(result, Orphans)
	if len(o) != 1 || o[0].Err != nil || o[0].Skipped != nil || !strings.Contains(o[0].Summary, "gone") {
		t.Errorf("orphans outcomes = %+v, want one that ran and reported gone", o)
	}
}

func TestConvergeNamesBothIndependentRootFailures(t *testing.T) {
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, declaresEverything(), t.TempDir(), true)
	want := []string{"placements ~/.netrc", "toolchain (box)"}
	if got := skippedLabels(result)["repos api"]; !reflect.DeepEqual(got, want) {
		t.Errorf("repos api skipped for %v, want %v", got, want)
	}
}

func TestReportCountsFailuresAndSkipsAndRestatesBoth(t *testing.T) {
	b := declaresEverything()
	b.Groups = nil
	result, _ := convergeDeclared(t, readyBox(), b, t.TempDir(), true)
	if !result.Failed() {
		t.Error("Result.Failed() = false, want true on a skip")
	}
	got := result.Report()
	for _, want := range []string{
		"1 failed, 2 skipped\n",
		"  placements  failed: ",
		"  repos       skipped: needs placements ~/.netrc, which failed\n  repos       skipped: needs placements ~/.netrc, which failed\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Report() = %q, want it to contain %q", got, want)
		}
	}
}

func TestSkipAloneLeavesTheRunFailed(t *testing.T) {
	r := Result{Outcomes: []Outcome{{Step: Repos, Skipped: []string{"x"}}}}
	if !r.Failed() {
		t.Error("Result.Failed() = false, want true")
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

func TestConvergeKeepsARepoNamedBoxApartFromTheBoxToolchain(t *testing.T) {
	plan := Plan(boxNamedRepoBlueprint(), resolved, "/home/smith")
	var override string
	for _, u := range plan {
		if u.Step == Toolchain && u.Fragment.Repo == "(box)" {
			override = u.Label
		}
	}
	if override == "toolchain (box)" || override == "" {
		t.Fatalf("override label = %q, want one distinct from the box toolchain", override)
	}
	home := t.TempDir()
	plan = Plan(boxNamedRepoBlueprint(), resolved, home)
	var fragDir string
	for _, u := range plan {
		if u.Label == override {
			fragDir = u.Fragment.Dir
		}
	}
	result, _ := convergeDeclared(t, failingInstall{readyBox(), fragDir}, boxNamedRepoBlueprint(), home, false)
	want := map[string][]string{"repos (box)": {override}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestConvergeSkipsEverythingOnABoxToolchainFailureBesideARepoNamedBox(t *testing.T) {
	box := newBox()
	box.miseErr = errors.New("no network")
	result, _ := convergeDeclared(t, box, boxNamedRepoBlueprint(), t.TempDir(), false)
	for _, o := range result.Outcomes {
		if (o.Step == Repos || o.Step == Toolchain && o.Label != "toolchain (box)") &&
			!reflect.DeepEqual(o.Skipped, []string{"toolchain (box)"}) {
			t.Errorf("%s skipped = %v, want [toolchain (box)]", o.Label, o.Skipped)
		}
	}
}
