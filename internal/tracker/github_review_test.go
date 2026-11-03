package tracker_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestExecutionReadsTheReviewMarksOfASpec(t *testing.T) {
	t.Parallel()
	replies := recorded()
	replies[specView(42)] = strings.Replace(replies[specView(42)], `"labels":[`, `"labels":[{"name":"smith:reviewed"},{"name":"smith:reviewing"},`, 1)

	got, err := tracker.NewGitHub(&fakeGH{replies: replies}, tracker.DefaultLabels()).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	if !got.Reviewed || !got.Reviewing {
		t.Errorf("Execution(42) reviewed %v, reviewing %v; want both marks read", got.Reviewed, got.Reviewing)
	}
}

func TestExecutionReadsTheReviewMarksOfAStandaloneTask(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		specView(80): `{"body":"","labels":[{"name":"ready-for-agent"},{"name":"smith:reviewed"}],"number":80,"state":"CLOSED","url":"https://github.com/o/r/issues/80","subIssues":{"nodes":[],"totalCount":0},"title":"Fix X"}`,
		taskView(80): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"},{"name":"smith:reviewed"}],"number":80,"state":"CLOSED","title":"Fix X","url":"https://github.com/o/r/issues/80"}`,
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 80)
	if err != nil {
		t.Fatalf("Execution(80) error = %v", err)
	}

	if !got.Reviewed || got.Reviewing || !got.Standalone() {
		t.Errorf("Execution(80) = %+v, standalone %v; want a standalone task marked reviewed", got, got.Standalone())
	}
}

func TestExecutionOfAChildTaskIsNotStandalone(t *testing.T) {
	t.Parallel()

	got, err := tracker.NewGitHub(childOf(74, "Spec: Widgets"), tracker.DefaultLabels()).Execution(t.Context(), 82)
	if err != nil {
		t.Fatalf("Execution(82) error = %v", err)
	}

	if got.Standalone() {
		t.Errorf("Execution(82).Standalone() = true, want false for a child task run alone")
	}
}
func TestTheReviewMarksAreOneEditEach(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:reviewing", "smith:reviewed"}}
	github := tracker.NewGitHub(gh, tracker.DefaultLabels())

	for _, err := range []error{github.StartReview(t.Context(), 42), github.StopReview(t.Context(), 42), github.MarkReviewed(t.Context(), 42)} {
		if err != nil {
			t.Fatalf("review mark error = %v", err)
		}
	}

	want := []string{
		"issue edit 42 --add-label smith:reviewing",
		"issue edit 42 --remove-label smith:reviewing",
		"issue edit 42 --add-label smith:reviewed",
	}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("review marks ran gh %q, want %q", gh.ran, want)
	}
}

func TestStartReviewCreatesAMissingLabelOnFirstUse(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).StartReview(t.Context(), 42); err != nil {
		t.Fatalf("StartReview(42) error = %v", err)
	}

	if !slices.Contains(gh.labels, "smith:reviewing") {
		t.Errorf("repo labels = %q, want smith:reviewing created", gh.labels)
	}
}

func TestTheReviewMarksAreNotTriageLabels(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"smith:reviewing", "smith:reviewed"} {
		if !tracker.IsMark(name) {
			t.Errorf("IsMark(%q) = false, want true", name)
		}
	}
}
