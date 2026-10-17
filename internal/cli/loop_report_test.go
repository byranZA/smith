package cli

import (
	"slices"
	"strings"
	"testing"
)

// commentsOn is each gh issue comment gh ran on issue number.
func commentsOn(gh *fakeTracker, number string) []string {
	return slices.DeleteFunc(slices.Clone(gh.ran), func(ran string) bool { return !strings.HasPrefix(ran, "gh issue comment "+number+" ") })
}

func TestLoopRunPostsItsStopReportOnTheSpec(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}

	runLoopRun(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, "42")

	want := []string{"gh issue comment 42 --body smith stopped its run of this spec: **complete**.\n\n```text\nspec #42 complete\n" + workBranch + "```\n"}
	if got := commentsOn(gh, "42"); !slices.Equal(got, want) {
		t.Errorf("gh commented %q on #42, want %q", got, want)
	}
}

func TestLoopRunInteractivePostsNoStopReport(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}

	runLoopRun(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, "42", "--interactive")

	if got := commentsOn(gh, "42"); len(got) != 0 {
		t.Errorf("gh commented %q on #42, want no comment", got)
	}
}
