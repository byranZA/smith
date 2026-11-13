package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

func TestRenderFillsInTheTask(t *testing.T) {
	prompt := loop.Prompt("Do #{{TASK_NUMBER}} ({{TASK_TITLE}}); close #{{TASK_NUMBER}}.")

	got := prompt.Render(tracker.Task{Number: 43, Title: "Loop: run"}, "")

	if want := "Do #43 (Loop: run); close #43."; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestTheBuiltInPromptNamesTheTaskAndPointsToItsParent(t *testing.T) {
	got := loop.BuiltinPrompt().Render(tracker.Task{Number: 43, Title: "Loop: run"}, "")

	for _, want := range []string{"#43", "Loop: run", "`parent:`"} {
		if !strings.Contains(got, want) {
			t.Errorf("built-in prompt is missing %q", want)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("built-in prompt left a placeholder unfilled:\n%s", got)
	}
}

func TestAnInteractivePromptCarriesTheInteractiveNoteAfterTheTask(t *testing.T) {
	got := loop.Prompt("Do #{{TASK_NUMBER}}.").Interactive().Render(tracker.Task{Number: 43}, "")

	if !strings.HasPrefix(got, "Do #43.\n") || !strings.Contains(got, "A human is at the terminal") {
		t.Errorf("interactive prompt = %q, want the task followed by the interactive note", got)
	}
}

func TestAnEjectedPromptReplacesTheBuiltInWithItsPlaceholdersFilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("Do #{{TASK_NUMBER}}. Run make check before closing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, err := loop.LoadPrompt(path)
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if got, want := prompt.Render(tracker.Task{Number: 43}, ""), "Do #43. Run make check before closing.\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestWithoutAnEjectedPromptTheBuiltInIsUsed(t *testing.T) {
	prompt, err := loop.LoadPrompt(filepath.Join(t.TempDir(), "prompt.md"))
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if prompt != loop.BuiltinPrompt() {
		t.Errorf("LoadPrompt() = %q, want the built-in prompt", prompt)
	}
}

func TestAnEjectedCopyOfTheBuiltInRendersTheSameAsTheBuiltIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte(loop.BuiltinPrompt()), 0o644); err != nil {
		t.Fatal(err)
	}
	task := tracker.Task{Number: 43, Title: "Loop: run"}

	prompt, err := loop.LoadPrompt(path)
	if err != nil {
		t.Fatalf("LoadPrompt() error = %v", err)
	}

	if got, want := prompt.Interactive().Render(task, ""), loop.BuiltinPrompt().Interactive().Render(task, ""); got != want {
		t.Errorf("ejected copy renders\n%s\nwant\n%s", got, want)
	}
}

func TestAnUnknownPlaceholderInAnEjectedPromptIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("Do #{{TASK_NUMBER}}, see {{ISSUE_URL}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loop.LoadPrompt(path)

	want := []string{path, "{{ISSUE_URL}}", "known placeholders are {{TASK_NUMBER}}, {{TASK_TITLE}}, {{REPORT_FILE}}\n"}
	for _, w := range want {
		if err == nil || !strings.Contains(err.Error()+"\n", w) {
			t.Errorf("LoadPrompt() error = %v, want one naming %q", err, w)
		}
	}
}

func TestAnEjectedPromptHoldingTheSpecPlaceholderIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("Do #{{TASK_NUMBER}} of #{{SPEC_NUMBER}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loop.LoadPrompt(path)

	want := path + ": unknown placeholder {{SPEC_NUMBER}}: known placeholders are {{TASK_NUMBER}}, {{TASK_TITLE}}, {{REPORT_FILE}}"
	if err == nil || err.Error() != want {
		t.Errorf("LoadPrompt() error = %v, want %q", err, want)
	}
}

func TestParsePromptRefusesAnUnknownPlaceholderNamingItsSource(t *testing.T) {
	_, err := loop.ParsePrompt("main:.smith/prompt.md", []byte("Do {{TASK}}.\n"))

	want := "main:.smith/prompt.md: unknown placeholder {{TASK}}: known placeholders are {{TASK_NUMBER}}, {{TASK_TITLE}}, {{REPORT_FILE}}"
	if err == nil || err.Error() != want {
		t.Errorf("ParsePrompt() err = %v, want %q", err, want)
	}
}

func TestParsePromptKeepsTheEjectedText(t *testing.T) {
	got, err := loop.ParsePrompt("main:.smith/prompt.md", []byte("Do #{{TASK_NUMBER}}.\n"))

	if err != nil || got != "Do #{{TASK_NUMBER}}.\n" {
		t.Errorf("ParsePrompt() = %q, %v; want the text as given", got, err)
	}
}

func TestParseReviewPromptRefusesAnUnknownPlaceholderNamingItsSource(t *testing.T) {
	_, err := loop.ParseReviewPrompt("main:.smith/review.md", []byte("Review {{TASK_NUMBER}}.\n"))

	want := "main:.smith/review.md: unknown placeholder {{TASK_NUMBER}}: known placeholders are {{ISSUE_NUMBER}}, {{ISSUE_TITLE}}, {{MERGE_BASE}}, {{FINDINGS_FILE}}"
	if err == nil || err.Error() != want {
		t.Errorf("ParseReviewPrompt() err = %v, want %q", err, want)
	}
}

func TestParseReviewPromptRequiresNoPlaceholder(t *testing.T) {
	got, err := loop.ParseReviewPrompt("review.md", []byte("Run our review skill.\n"))

	if err != nil || got != "Run our review skill.\n" {
		t.Errorf("ParseReviewPrompt() = %q, %v; want the text as given", got, err)
	}
}

func TestLoadReviewPromptReadsAnEjectedFileAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(path, []byte("Our review of #{{ISSUE_NUMBER}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ejected, err := loop.LoadReviewPrompt(path)

	if err != nil || !ejected || got != "Our review of #{{ISSUE_NUMBER}}.\n" {
		t.Errorf("LoadReviewPrompt() = %q, %v, %v; want the file's text, ejected", got, ejected, err)
	}
}

func TestLoadReviewPromptFallsBackToTheBuiltInWhenNothingIsEjected(t *testing.T) {
	got, ejected, err := loop.LoadReviewPrompt(filepath.Join(t.TempDir(), "review.md"))

	if err != nil || ejected || got != loop.BuiltinReviewPrompt() {
		t.Errorf("LoadReviewPrompt() ejected = %v, err = %v, built-in = %v; want the built-in, not ejected", ejected, err, got == loop.BuiltinReviewPrompt())
	}
}

func TestTheBuiltInPromptAsksForTheTwoPartReportAtTheReportFile(t *testing.T) {
	got := loop.BuiltinPrompt().Render(tracker.Task{Number: 43, Title: "Loop: run"}, "/repo/.git/smith/report-43.md")

	for _, want := range []string{
		"/repo/.git/smith/report-43.md",
		"**The task**", "**The environment**",
		"Done", "Deviations / skipped", "Left open", "For review", "Friction", "Recommendations",
		"None",
		"`gh issue close 43`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("built-in prompt is missing %q", want)
		}
	}
}

func TestTheBuiltInPromptNoLongerAsksForAWIPNoteOrACloseComment(t *testing.T) {
	got := loop.BuiltinPrompt().Render(tracker.Task{Number: 43, Title: "Loop: run"}, "/r.md")

	for _, banned := range []string{"WIP:", "gh issue comment", "--comment \"Done in"} {
		if strings.Contains(got, banned) {
			t.Errorf("built-in prompt still holds %q", banned)
		}
	}
}
