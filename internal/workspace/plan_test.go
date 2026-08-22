package workspace

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// TestOrderIsTheStageOrder pins the order the stage converges in. It is not
// arbitrary: placements put the box's git identity and forge credentials on
// disk before a clone needs them, and the toolchain exports the blueprint's env
// before a clone runs under it.
func TestOrderIsTheStageOrder(t *testing.T) {
	want := "placements,packages,toolchain,repos"
	got := make([]string, 0, len(Order()))
	for _, s := range Order() {
		got = append(got, string(s))
	}
	if strings.Join(got, ",") != want {
		t.Errorf("Order() = %q, want %q", strings.Join(got, ","), want)
	}
}

// TestPlan drives the pure derivation from a staged blueprint to the ordered
// units of work the stage converges.
func TestPlan(t *testing.T) {
	tests := []struct {
		name string
		b    blueprint.Blueprint
		want []Unit
	}{
		{
			name: "a blueprint declaring nothing plans nothing",
			b:    blueprint.Blueprint{},
			want: nil,
		},
		{
			name: "declared packages plan one packages unit",
			b:    blueprint.Blueprint{Packages: []string{"ripgrep", "jq"}},
			want: []Unit{{Step: Packages, Packages: []string{"ripgrep", "jq"}}},
		},
		{
			name: "an empty packages list plans nothing",
			b:    blueprint.Blueprint{Packages: []string{}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Plan(tt.b)
			if len(got) != len(tt.want) {
				t.Fatalf("Plan() = %v, want %v", got, tt.want)
			}
			for i, unit := range got {
				if unit.Step != tt.want[i].Step {
					t.Errorf("Plan()[%d].Step = %q, want %q", i, unit.Step, tt.want[i].Step)
				}
				if strings.Join(unit.Packages, ",") != strings.Join(tt.want[i].Packages, ",") {
					t.Errorf("Plan()[%d].Packages = %v, want %v", i, unit.Packages, tt.want[i].Packages)
				}
			}
		})
	}
}

// TestPlanIsPure proves a caller that mutates a planned unit cannot reach back
// into the blueprint it was planned from.
func TestPlanIsPure(t *testing.T) {
	b := blueprint.Blueprint{Packages: []string{"ripgrep"}}
	Plan(b)[0].Packages[0] = "mutated"
	if b.Packages[0] != "ripgrep" {
		t.Errorf("blueprint packages = %v after a caller mutated the plan, want [ripgrep]", b.Packages)
	}
}

// TestPlanFollowsTheStageOrder proves the units come back in the order the
// stage converges them, whatever order the blueprint happens to declare its
// fields in.
func TestPlanFollowsTheStageOrder(t *testing.T) {
	b := blueprint.Blueprint{Packages: []string{"ripgrep"}}
	plan := Plan(b)
	at := make(map[Step]int, len(Order()))
	for i, s := range Order() {
		at[s] = i
	}
	for i := 1; i < len(plan); i++ {
		if at[plan[i-1].Step] > at[plan[i].Step] {
			t.Errorf("Plan() step %q precedes %q, which is out of stage order", plan[i-1].Step, plan[i].Step)
		}
	}
}
