package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/staging"
)

// ada is the identity an operator declared in full.
var ada = staging.Identity{UserName: "Ada", UserEmail: "ada@example.com"}

// convergeIdentity runs the stage over one identity unit against the box.
func convergeIdentity(t *testing.T, box *fakeBox, id staging.Identity) Result {
	t.Helper()
	var progress bytes.Buffer
	result, err := Converge(context.Background(), Env{Command: box}, []Unit{{Step: Identity, Identity: id}}, &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result
}

func TestConvergeSetsTheDeclaredIdentity(t *testing.T) {
	box := newBox()
	result := convergeIdentity(t, box, ada)

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if got := box.gitConfig["user.name"]; got != "Ada" {
		t.Errorf("user.name = %q, want %q", got, "Ada")
	}
	if got := box.gitConfig["user.email"]; got != "ada@example.com" {
		t.Errorf("user.email = %q, want %q", got, "ada@example.com")
	}
}

func TestConvergeIdentityKeepsTheRestOfTheGitConfig(t *testing.T) {
	box := newBox()
	box.gitConfig["init.defaultBranch"] = "main"
	convergeIdentity(t, box, ada)

	if got := box.gitConfig["init.defaultBranch"]; got != "main" {
		t.Errorf("init.defaultBranch = %q, want %q", got, "main")
	}
}

func TestConvergeIdentityReplacesAChangedValue(t *testing.T) {
	box := newBox()
	box.gitConfig["user.email"] = "old@example.com"
	result := convergeIdentity(t, box, staging.Identity{UserEmail: "new@example.com"})

	if got := box.gitConfig["user.email"]; got != "new@example.com" {
		t.Errorf("user.email = %q, want %q", got, "new@example.com")
	}
	if summary := result.Outcomes[0].Summary; summary != "set user.email" {
		t.Errorf("outcome summary = %q, want %q", summary, "set user.email")
	}
}

func TestConvergeIdentityLeavesAnUndeclaredFieldAsItWas(t *testing.T) {
	box := newBox()
	box.gitConfig["user.name"] = "Grace"
	convergeIdentity(t, box, staging.Identity{UserEmail: "ada@example.com"})

	if got := box.gitConfig["user.name"]; got != "Grace" {
		t.Errorf("user.name = %q, want it left as %q", got, "Grace")
	}
	if box.ran("user.name") {
		t.Errorf("the stage ran %v, want nothing touching user.name", box.calls)
	}
}

func TestConvergeIdentityReportsUnchangedWhenAlreadySet(t *testing.T) {
	box := newBox()
	box.gitConfig["user.name"] = "Ada"
	box.gitConfig["user.email"] = "ada@example.com"
	result := convergeIdentity(t, box, ada)

	if summary := result.Outcomes[0].Summary; summary != "user.name, user.email already set" {
		t.Errorf("outcome summary = %q, want %q", summary, "user.name, user.email already set")
	}
	for _, argv := range box.calls {
		if !strings.Contains(strings.Join(argv, " "), "--get") {
			t.Errorf("the stage ran %q on an already-converged identity, want only reads", strings.Join(argv, " "))
		}
	}
}

func TestConvergeIdentityReportsUnchangedWhenAnIncludedFileHoldsIt(t *testing.T) {
	box := newBox()
	box.included["user.name"] = "Ada"
	box.included["user.email"] = "ada@example.com"
	result := convergeIdentity(t, box, ada)

	if summary := result.Outcomes[0].Summary; summary != "user.name, user.email already set" {
		t.Errorf("outcome summary = %q, want %q", summary, "user.name, user.email already set")
	}
	for _, argv := range box.calls {
		if !strings.Contains(strings.Join(argv, " "), "--get") {
			t.Errorf("the stage ran %q on an identity an include already holds, want only reads", strings.Join(argv, " "))
		}
	}
}

func TestConvergeIdentityFailsWhenALaterIncludeOverridesIt(t *testing.T) {
	box := newBox()
	box.gitConfig["user.email"] = "old@example.com"
	box.included["user.email"] = "grace@example.com"

	for run := range 2 {
		result := convergeIdentity(t, box, staging.Identity{UserEmail: "ada@example.com"})
		o := result.Outcomes[0]
		if !result.Failed() || o.Step != Identity || o.Err == nil {
			t.Fatalf("run %d: outcome = %+v, want the identity step failed while git commits as grace", run+1, o)
		}
		if !strings.Contains(o.Err.Error(), "grace@example.com") {
			t.Errorf("run %d: error = %q, want it to name the identity git commits with", run+1, o.Err)
		}
	}
}

func TestConvergeIdentityReportsAFailedWrite(t *testing.T) {
	box := newBox()
	box.gitConfigErr = errors.New("error: could not lock config file /home/smith/.gitconfig: Permission denied")
	result := convergeIdentity(t, box, ada)

	if !result.Failed() {
		t.Fatalf("Result.Failed() = false, want the failed write reported: %s", result.Report())
	}
	if o := result.Outcomes[0]; o.Step != Identity || o.Err == nil || !strings.Contains(o.Err.Error(), "Permission denied") {
		t.Errorf("outcome = %+v, want the identity step failed with git's own error", o)
	}
	if report := result.Report(); !strings.Contains(report, "identity") {
		t.Errorf("Report() = %q, want it to name the identity step", report)
	}
}

func TestConvergeIdentityFailsOnAnUnexpectedReadBeforeWriting(t *testing.T) {
	box := newBox()
	box.gitReadErr = exitStatus(128)
	box.gitReadStderr = "fatal: bad config line 3 in file /home/smith/.gitconfig\n"
	result := convergeIdentity(t, box, ada)

	o := result.Outcomes[0]
	if !result.Failed() || o.Step != Identity || o.Err == nil {
		t.Fatalf("outcome = %+v, want the identity step failed on the read", o)
	}
	for _, want := range []string{"user.name", "bad config line 3"} {
		if !strings.Contains(o.Err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", o.Err, want)
		}
	}
	if len(box.gitConfig) != 0 {
		t.Errorf("git config = %v, want nothing written after a failed read", box.gitConfig)
	}
}

func TestConvergeIdentityPropagatesCancellation(t *testing.T) {
	box := newBox()
	box.gitReadErr = exitStatus(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := setIdentity(ctx, box, ada, io.Discard)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("setIdentity() error = %v, want %v", err, context.Canceled)
	}
	if len(box.gitConfig) != 0 {
		t.Errorf("git config = %v, want nothing written after cancellation", box.gitConfig)
	}
}

func TestPlanIdentity(t *testing.T) {
	tests := []struct {
		name string
		git  staging.Identity
		want []staging.Identity
	}{
		{"a declared identity plans one identity unit", ada, []staging.Identity{ada}},
		{"half an identity plans that half", staging.Identity{UserEmail: "ada@example.com"}, []staging.Identity{{UserEmail: "ada@example.com"}}},
		{"no declared identity plans no identity unit", staging.Identity{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := resolved
			r.Git = tt.git
			var got []staging.Identity
			for _, unit := range Plan(blueprint.Blueprint{}, r, "/home/smith") {
				if unit.Step == Identity {
					got = append(got, unit.Identity)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Plan() planned %d identity units, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Plan() identity %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPlanSetsTheIdentityAfterThePlacements(t *testing.T) {
	b := blueprint.Blueprint{
		Packages:   []string{"ripgrep"},
		Placements: []blueprint.Placement{{From: "file:/home/op/.npmrc", To: "~/.npmrc"}},
	}
	r := resolved
	r.Git = ada
	var got []string
	for _, unit := range Plan(b, r, "/home/smith") {
		got = append(got, string(unit.Step))
	}
	if want := "placements,identity,packages,orphans"; strings.Join(got, ",") != want {
		t.Errorf("Plan() steps = %q, want %q", strings.Join(got, ","), want)
	}
}
