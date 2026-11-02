package workspace

import (
	"context"
	"errors"
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

// convergeSkipping converges the plan with the named units failing and the rest succeeding.
func convergeSkipping(t *testing.T, plan []Unit, fail ...string) (Result, string) {
	t.Helper()
	var progress strings.Builder
	result, err := convergeWith(context.Background(), plan, func(u Unit) (string, error) {
		if slices.Contains(fail, u.Label) {
			return "", errors.New("boom")
		}
		return "ok", nil
	}, &progress)
	if err != nil {
		t.Fatalf("convergeWith() error = %v", err)
	}
	return result, progress.String()
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
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, progress := convergeSkipping(t, plan, "placements ~/.netrc")
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
}

func TestConvergeSkipsRepoUnitsOnAFailedBoxToolchainNamingTheRoot(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "toolchain (box)")
	want := map[string][]string{
		"toolchain acme": {"toolchain (box)"},
		"repos acme":     {"toolchain (box)"},
		"repos api":      {"toolchain (box)"},
	}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestConvergeSkipsOnlyTheRepoWhoseOverrideFailed(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "toolchain acme")
	want := map[string][]string{"repos acme": {"toolchain acme"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestConvergeSkipsGroupsOnFailedPackagesButRunsTheRest(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "packages")
	want := map[string][]string{"groups": {"packages"}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestConvergeKeepsSiblingReposConvergingPastAFailure(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "repos acme")
	if got := skippedLabels(result); len(got) != 0 {
		t.Errorf("skipped = %v, want none", got)
	}
	if o := outcomesFor(result, Repos); len(o) != 2 || o[0].Err == nil || o[1].Err != nil {
		t.Errorf("repo outcomes = %+v, want acme failed and api converged", o)
	}
}

func TestConvergeStillScansOrphansAfterFailures(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "toolchain (box)")
	if o := outcomesFor(result, Orphans); len(o) != 1 || o[0].Err != nil || o[0].Skipped != nil {
		t.Errorf("orphans outcomes = %+v, want one that ran", o)
	}
}

func TestConvergeNamesBothIndependentRootFailures(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "placements ~/.netrc", "toolchain (box)")
	want := []string{"placements ~/.netrc", "toolchain (box)"}
	if got := skippedLabels(result)["repos api"]; !reflect.DeepEqual(got, want) {
		t.Errorf("repos api skipped for %v, want %v", got, want)
	}
}

func TestReportCountsFailuresAndSkipsAndRestatesBoth(t *testing.T) {
	plan := Plan(declaresEverything(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "placements ~/.netrc")
	if !result.Failed() {
		t.Error("Result.Failed() = false, want true on a skip")
	}
	want := "workspace: 5 steps converged, 1 failed, 2 skipped\n" +
		"  placements  failed: boom\n" +
		"  repos       skipped: needs placements ~/.netrc, which failed\n" +
		"  repos       skipped: needs placements ~/.netrc, which failed\n"
	if got := result.Report(); got != want {
		t.Errorf("Report() = %q, want %q", got, want)
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
	result, _ := convergeSkipping(t, plan, override)
	want := map[string][]string{"repos (box)": {override}}
	if got := skippedLabels(result); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestConvergeSkipsEverythingOnABoxToolchainFailureBesideARepoNamedBox(t *testing.T) {
	plan := Plan(boxNamedRepoBlueprint(), resolved, "/home/smith")
	result, _ := convergeSkipping(t, plan, "toolchain (box)")
	for _, o := range result.Outcomes {
		if (o.Step == Repos || o.Step == Toolchain && o.Label != "toolchain (box)") &&
			!reflect.DeepEqual(o.Skipped, []string{"toolchain (box)"}) {
			t.Errorf("%s skipped = %v, want [toolchain (box)]", o.Label, o.Skipped)
		}
	}
}
