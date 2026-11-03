package review_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/review"
)

func TestParseAcceptsAFileWithNoFindings(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"findings": [], "not_filed": []}`,
		`{"findings": []}`,
	} {
		got, err := review.Parse([]byte(data))
		if err != nil || len(got.Findings) != 0 {
			t.Errorf("Parse(%s) = %+v, %v; want a file with no findings", data, got, err)
		}
	}
}

func TestParseRefusesAFileItCannotRead(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"not JSON":             `not json`,
		"no findings key":      `{"not_filed": []}`,
		"an unknown key":       `{"findings": [], "extra": 1}`,
		"a restructuring bare": `{"findings": [], "not_filed": [{"title": "", "summary": "s"}]}`,
		"findings to file":     `{"findings": [{"axis": "spec", "severity": "major", "title": "t", "who": "agent", "body": "b"}]}`,
	} {
		if _, err := review.Parse([]byte(data)); err == nil {
			t.Errorf("Parse(%s) error = nil, want one for %s", data, name)
		}
	}
}

func TestCommentReadsByAxisWithTheLedgerHidden(t *testing.T) {
	t.Parallel()
	file, err := review.Parse([]byte(`{"findings": [], "not_filed": [{"title": "Split the loop", "summary": "Too big for a task."}]}`))
	if err != nil {
		t.Fatal(err)
	}

	got := review.Comment(file, 42, "abc123")

	for _, want := range []string{
		"## Spec\n\nNo findings.",
		"## Standards\n\nNo findings.",
		"## Quality\n\nNo findings.",
		"**Not filed**",
		"Split the loop",
		"<!-- smith:review ",
		"abc123",
		"review of #42",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Comment() = %q, want it to hold %q", got, want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "-->") {
		t.Errorf("Comment() = %q, want the hidden ledger block last", got)
	}
}
