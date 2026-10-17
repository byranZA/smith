package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
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

func TestLoopRunNamesATaskWhoseAgentWasKilledInTheReport(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: killedAgent{}, lookPath: onPath}, "42")

	want := defaultSettings + "spec #42 stopped: the agent left a task open on every attempt\n" +
		"  skipped after 2 attempts: #43\n" +
		"  #43 left in /bench/43: the agent was killed (likely out of memory)\n" + workBranch
	if code == 0 || stdout != want {
		t.Errorf("exit %d, stdout =\n%s\nwant non-zero, and\n%s", code, stdout, want)
	}
}

// killedAgent is a launcher whose agent is killed by SIGKILL without closing its task.
type killedAgent struct{}

func (killedAgent) Launch(context.Context, agent.Command) error {
	return fmt.Errorf("run claude: %w", loop.ErrKilled)
}
