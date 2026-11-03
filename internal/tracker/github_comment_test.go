package tracker_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestCommentPostsTheBodyOnTheIssue(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Comment(t.Context(), 12, "smith skipped this task."); err != nil {
		t.Fatalf("Comment(12) error = %v", err)
	}

	if want := []string{"issue comment 12 --body smith skipped this task."}; !slices.Equal(gh.ran, want) {
		t.Errorf("Comment(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestCommentReportsARefusedComment(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{stderr: "HTTP 403: Resource not accessible by integration\n"}

	err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Comment(t.Context(), 12, "smith skipped this task.")

	var unavailable *tracker.UnavailableError
	if !errors.As(err, &unavailable) || !strings.Contains(unavailable.Error(), "HTTP 403") {
		t.Errorf("Comment(12) error = %v, want an UnavailableError carrying what gh said", err)
	}
}

func TestExecutionReadsTheCommentsOfASpecWithTheNumericIDsEditsNeed(t *testing.T) {
	t.Parallel()
	replies := recorded()
	replies[specView(42)] = strings.Replace(replies[specView(42)], `"labels":[`, `"comments":[{"id":"IC_a","url":"https://github.com/o/r/issues/42#issuecomment-1001","body":"first"},{"id":"IC_b","url":"https://github.com/o/r/issues/42#issuecomment-1002","body":"second"}],"labels":[`, 1)

	got, err := tracker.NewGitHub(&fakeGH{replies: replies}, tracker.DefaultLabels()).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	want := []tracker.Comment{{ID: 1001, Body: "first"}, {ID: 1002, Body: "second"}}
	if !reflect.DeepEqual(got.Comments, want) {
		t.Errorf("Execution(42).Comments = %+v, want %+v", got.Comments, want)
	}
}
