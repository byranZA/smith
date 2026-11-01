package agentguide_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agentguide"
)

func fixture() ([]agentguide.Verb, []agentguide.ExitCode) {
	verbs := []agentguide.Verb{
		{Path: "smith widget make", Summary: "Make a widget", Relays: true, RunsLong: true},
		{Path: "smith widget peek", Summary: "Look at a widget", NeedsTerminal: true},
		{Path: "smith widget plain", Summary: "Do a plain thing"},
	}
	codes := []agentguide.ExitCode{{Code: 0, Meaning: "All went well."}, {Code: 77, Meaning: "The widget broke."}}
	return verbs, codes
}

func TestRenderListsEveryVerbWithItsTraits(t *testing.T) {
	t.Parallel()
	verbs, codes := fixture()
	got := agentguide.Render(verbs, codes)
	for _, want := range []string{
		"- `smith widget make`: Make a widget (relays to a box; runs long)",
		"- `smith widget peek`: Look at a widget (needs a terminal)",
		"- `smith widget plain`: Do a plain thing\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing line %q in:\n%s", want, got)
		}
	}
}

func TestRenderExplainsEveryExitCode(t *testing.T) {
	t.Parallel()
	verbs, codes := fixture()
	got := agentguide.Render(verbs, codes)
	for _, want := range []string{"- `0`: All went well.", "- `77`: The widget broke."} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing line %q in:\n%s", want, got)
		}
	}
}

func TestRenderSaysWhoTheGuideIsFor(t *testing.T) {
	t.Parallel()
	verbs, codes := fixture()
	got := agentguide.Render(verbs, codes)
	for _, want := range []string{"for an agent that drives smith", "handed a task by the loop does not run smith"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in:\n%s", want, got)
		}
	}
}
