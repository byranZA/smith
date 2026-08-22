package cli

import (
	"strings"
	"testing"
)

// TestSessionListRendersTheTableAndTheSummary is the smoke test for the verb:
// a session stood up a moment ago is rendered under the four columns, named as
// the other verbs take it, and counted in the summary line. Which sessions the
// box holds is internal/session's to prove.
func TestSessionListRendersTheTableAndTheSummary(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "smith/spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, &fakeExec{}), "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"NAME", "STATE", "DIRTY", "UNPUSHED", "smith-smith-spec-42", "1 session"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "smith/spec-42") {
		t.Errorf("stdout = %q, want the true branch left out of the default output", stdout)
	}
}

// TestSessionListSaysSoWhenThereIsNothingToList locks in that an empty box
// still prints the summary line and still exits zero.
func TestSessionListSaysSoWhenThereIsNothingToList(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")

	stdout, stderr, code := runSessionOn(t,
		onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{}), "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 whatever list finds (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "no sessions") {
		t.Errorf("stdout = %q, want it to report that there are no sessions", stdout)
	}
}

// TestSessionListExitsZeroWithUnpushedWork locks in that list reports what it
// found rather than failing on it: a non-zero exit on "something is unpushed"
// would conflate the command failing with the data having a property. How the
// unpushed work is counted and worded is internal/session's to prove.
func TestSessionListExitsZeroWithUnpushedWork(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, &fakeExec{}), "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 with unpushed work present (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want the session with unpushed work listed", stdout)
	}
}

// TestSessionListFlagsNarrowTheRows locks in that each filter flag reaches the
// listing it narrows. Which rows a given filter selects is internal/session's
// to prove; that --repo, --live and --stopped are the flags that carry it is
// the command's.
func TestSessionListFlagsNarrowTheRows(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)

	tests := []struct {
		name   string
		args   []string
		want   []string
		absent []string
	}{
		{"by repo", []string{"list", "--repo", "smith"}, []string{"smith-live-one", "smith-spec-42"}, []string{"web-hotfix"}},
		{"live only", []string{"list", "--live"}, []string{"smith-live-one"}, []string{"smith-spec-42", "web-hotfix"}},
		{"stopped only", []string{"list", "--stopped"}, []string{"smith-spec-42", "web-hotfix"}, []string{"smith-live-one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), tt.args...)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, want it to list %q", stdout, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(stdout, absent) {
					t.Errorf("stdout = %q, want %q filtered out", stdout, absent)
				}
			}
		})
	}
}

// TestSessionListRefusesTwoOppositeStateFilters locks in that a filter pair
// that can never match anything is told plainly, rather than answered with an
// empty table that reads like a box with no sessions.
func TestSessionListRefusesTwoOppositeStateFilters(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)

	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "list", "--live", "--stopped")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for two opposite state filters")
	}
	if !strings.Contains(stderr, "--stopped") {
		t.Errorf("stderr = %q, want it to name the filters that contradict", stderr)
	}
}

// TestSessionListNamesPrintsBareNames locks in the one output that is not for
// a human to read: names alone, one per line, so the shell can hand them
// straight to the removal verb.
func TestSessionListNamesPrintsBareNames(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "list", "--names")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "smith-live-one\nsmith-spec-42\nweb-hotfix\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestSessionListIsUnderTheRootCommand locks in the surface an operator on the
// box types.
func TestSessionListIsUnderTheRootCommand(t *testing.T) {
	underRoot(t, "list")
}
