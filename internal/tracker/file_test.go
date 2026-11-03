package tracker_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

// scriptedGH answers each gh call with the next of its replies, recording the
// arguments it was run with.
type scriptedGH struct {
	ran     [][]string
	replies []reply
}

type reply struct {
	stdout, stderr string
	err            error
}

func (s *scriptedGH) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	s.ran = append(s.ran, args)
	r := reply{}
	if len(s.replies) > 0 {
		r, s.replies = s.replies[0], s.replies[1:]
	}
	_, _ = io.WriteString(stdout, r.stdout)
	_, _ = io.WriteString(stderr, r.stderr)
	return r.err
}

func TestFileCreatesASubIssueWithTheAudiencesLabelAndBlockers(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{replies: []reply{{stdout: "https://github.com/o/r/issues/101\n"}}}
	labels := tracker.Labels{Agent: "ai-ready", Human: "needs-human", Spec: "spec"}

	number, err := tracker.NewGitHub(gh, labels).File(t.Context(), 42, tracker.NewTask{Title: "T", Body: "B", For: tracker.Agent, BlockedBy: []int{100}})

	want := []string{"issue", "create", "--title", "T", "--body", "B", "--label", "ai-ready", "--parent", "42", "--blocked-by", "100"}
	if err != nil || number != 101 || len(gh.ran) != 1 || !slices.Equal(gh.ran[0], want) {
		t.Errorf("File() = %d, %v after %q; want #101 after %q", number, err, gh.ran, want)
	}
}

func TestFileLabelsAHumanTaskForTheHuman(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{replies: []reply{{stdout: "https://github.com/o/r/issues/7\n"}}}

	if _, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).File(t.Context(), 42, tracker.NewTask{Title: "T", Body: "B", For: tracker.Human}); err != nil {
		t.Fatalf("File() error = %v", err)
	}

	if !slices.Contains(gh.ran[0], "ready-for-human") || slices.Contains(gh.ran[0], "--blocked-by") {
		t.Errorf("gh ran %q, want the human label and no blocked-by", gh.ran[0])
	}
}

func TestFileFallsBackToBodyLinesWhenGhLacksTheLinkFlags(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{replies: []reply{
		{stderr: "unknown flag: --parent\n", err: errors.New("exit status 1")},
		{stdout: "https://github.com/o/r/issues/102\n"},
	}}

	number, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).File(t.Context(), 42, tracker.NewTask{Title: "T", Body: "B", For: tracker.Agent, BlockedBy: []int{101}})

	if err != nil || number != 102 || len(gh.ran) != 2 {
		t.Fatalf("File() = %d, %v after %d runs; want #102 after 2", number, err, len(gh.ran))
	}
	retry := gh.ran[1]
	body := retry[slices.Index(retry, "--body")+1]
	if slices.Contains(retry, "--parent") || slices.Contains(retry, "--blocked-by") || !strings.HasPrefix(body, "Part of #42\nBlocked by #101\n\nB") {
		t.Errorf("retry ran %q, want no link flags and a body starting with the Part of and Blocked by lines", retry)
	}
}

func TestFileReportsAnIssueGhWouldNotCreate(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{replies: []reply{{stderr: "HTTP 502\n", err: errors.New("exit status 1")}}}

	_, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).File(t.Context(), 42, tracker.NewTask{Title: "T", Body: "B", For: tracker.Agent})

	var unavailable *tracker.UnavailableError
	if !errors.As(err, &unavailable) || len(gh.ran) != 1 {
		t.Errorf("File() error = %v after %d runs, want an UnavailableError after 1", err, len(gh.ran))
	}
}

func TestPostReturnsTheIDOfTheComment(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{replies: []reply{{stdout: "https://github.com/o/r/issues/42#issuecomment-9876\n"}}}

	id, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Post(t.Context(), 42, "body")

	want := []string{"issue", "comment", "42", "--body", "body"}
	if err != nil || id != 9876 || !slices.Equal(gh.ran[0], want) {
		t.Errorf("Post() = %d, %v after %q; want 9876 after %q", id, err, gh.ran, want)
	}
}

func TestEditCommentRewritesTheCommentByID(t *testing.T) {
	t.Parallel()
	gh := &scriptedGH{}

	err := tracker.NewGitHub(gh, tracker.DefaultLabels()).EditComment(t.Context(), 9876, "new body")

	want := []string{"api", "-X", "PATCH", "repos/{owner}/{repo}/issues/comments/9876", "-f", "body=new body"}
	if err != nil || !slices.Equal(gh.ran[0], want) {
		t.Errorf("EditComment() = %v after %q; want %q", err, gh.ran, want)
	}
}
