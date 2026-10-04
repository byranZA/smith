package tracker

import (
	"slices"
	"testing"
)

func TestListedTasksFollowTheTasksChecklist(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want []int
	}{
		"checklist order":         {"## Problem\n\nsee #9\n\n## Tasks\n\n- [ ] #45 last\n- [x] #43 first\n- [ ] #44\n\n## Notes\n\n#99\n", []int{45, 43, 44}},
		"repeats kept once":       {"## Tasks\n- [ ] #43\n- [ ] #43 again\n", []int{43}},
		"heading case and level":  {"### tasks\n- [ ] #7\n", []int{7}},
		"no checklist":            {"## Problem\n\n#12\n", nil},
		"empty body":              {"", nil},
		"section ends at heading": {"## Tasks\n- #1\n# Other\n- #2\n", []int{1}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := listedTasks(tc.body); !slices.Equal(got, tc.want) {
				t.Errorf("listedTasks() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBlockersInReadTheBlockedBySection(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want []int
	}{
		"listed":            {"## What\n\n#1\n\n## Blocked by\n\n- #43\n- #44 (the tracker)\n\n## Done\n#2\n", []int{43, 44}},
		"none":              {"## Blocked by\n\nNone - can start immediately\n", nil},
		"no section":        {"## What\n\nafter #43\n", nil},
		"url form":          {"## Blocked by\n\n- https://github.com/o/r/issues/43\n", []int{43}},
		"repeats kept once": {"## Blocked by\n#3 and #3\n", []int{3}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := blockersIn(tc.body); !slices.Equal(got, tc.want) {
				t.Errorf("blockersIn() = %v, want %v", got, tc.want)
			}
		})
	}
}
