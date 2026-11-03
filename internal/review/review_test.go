package review_test

import (
	"reflect"
	"slices"
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

	got := review.Comment(file, 42, "abc123", nil)

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

func finding(axis, severity, who string, blockedBy ...int) review.Finding {
	return review.Finding{Axis: axis, Severity: severity, Title: axis + " title", Who: who, Body: axis + " body", BlockedBy: blockedBy}
}

func TestParseRefusesAFindingThatIsNotWhole(t *testing.T) {
	t.Parallel()
	one := func(field, value string) string {
		f := map[string]string{"axis": "spec", "severity": "major", "title": "t", "who": "agent", "body": "b"}
		f[field] = value
		return `{"findings": [{"axis": "` + f["axis"] + `", "severity": "` + f["severity"] + `", "title": "` + f["title"] + `", "who": "` + f["who"] + `", "body": "` + f["body"] + `"}]}`
	}
	for name, data := range map[string]string{
		"an unknown axis":           one("axis", "style"),
		"an unknown severity":       one("severity", "critical"),
		"an unknown who":            one("who", "robot"),
		"a blank title":             one("title", " "),
		"a blank body":              one("body", ""),
		"a blocker off the list":    `{"findings": [{"axis": "spec", "severity": "major", "title": "t", "who": "agent", "body": "b", "blocked_by": [2]}]}`,
		"a finding blocking itself": `{"findings": [{"axis": "spec", "severity": "major", "title": "t", "who": "agent", "body": "b", "blocked_by": [1]}]}`,
		"blockers in a cycle": `{"findings": [
			{"axis": "spec", "severity": "major", "title": "a", "who": "agent", "body": "b", "blocked_by": [2]},
			{"axis": "spec", "severity": "major", "title": "c", "who": "agent", "body": "b", "blocked_by": [1]}]}`,
	} {
		if _, err := review.Parse([]byte(data)); err == nil {
			t.Errorf("Parse() error = nil, want one for %s", name)
		}
	}
}

func TestIDsCountEachAxisFromOne(t *testing.T) {
	t.Parallel()
	findings := []review.Finding{finding("quality", "minor", "agent"), finding("spec", "major", "agent"), finding("standards", "minor", "human"), finding("quality", "minor", "agent"), finding("spec", "minor", "agent")}

	got := review.IDs(findings)

	want := []string{"Q1", "S1", "St1", "Q2", "S2"}
	if !slices.Equal(got, want) {
		t.Errorf("IDs() = %v, want %v", got, want)
	}
}

func TestOrderFilesSpecThenStandardsThenQualityBlockersFirst(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		findings []review.Finding
		want     []int
	}{
		"by axis":        {[]review.Finding{finding("quality", "minor", "agent"), finding("standards", "minor", "agent"), finding("spec", "minor", "agent")}, []int{2, 1, 0}},
		"in file order":  {[]review.Finding{finding("quality", "minor", "agent"), finding("quality", "minor", "agent")}, []int{0, 1}},
		"blocker first":  {[]review.Finding{finding("spec", "minor", "agent", 2), finding("quality", "minor", "agent")}, []int{1, 0}},
		"chain of three": {[]review.Finding{finding("spec", "minor", "agent", 2), finding("spec", "minor", "agent", 3), finding("spec", "minor", "agent")}, []int{2, 1, 0}},
	} {
		if got := review.Order(tc.findings); !slices.Equal(got, tc.want) {
			t.Errorf("Order() for %s = %v, want %v", name, got, tc.want)
		}
	}
}

func TestTaskBodyEndsWithItsSourceLine(t *testing.T) {
	t.Parallel()
	got := review.TaskBody(finding("quality", "major", "agent"), "Q1", 42)

	want := "quality body\n\nSource: Quality axis, major, finding Q1, review of #42\n"
	if got != want {
		t.Errorf("TaskBody() = %q, want %q", got, want)
	}
}

func TestCommentListsFindingsByAxisTalliesThemAndNamesFiledTasks(t *testing.T) {
	t.Parallel()
	file := review.File{Findings: []review.Finding{finding("spec", "minor", "agent"), finding("spec", "blocker", "human"), finding("quality", "major", "agent")}}

	unfiled := review.Comment(file, 42, "abc", nil)
	filed := review.Comment(file, 42, "abc", map[int]int{0: 101, 1: 102, 2: 103})

	for _, want := range []string{"- **minor**: spec title", "- **blocker**: spec title", "Standards\n\nNo findings.", "2 Spec, 0 Standards, 1 Quality", "Worst: **blocker** spec title (S2)"} {
		if !strings.Contains(unfiled, want) {
			t.Errorf("Comment() = %q, want it to hold %q", unfiled, want)
		}
	}
	if strings.Contains(unfiled, "#101") {
		t.Errorf("Comment() = %q, want no task named before filing", unfiled)
	}
	for _, want := range []string{"(#101)", "(#102)", "(#103)"} {
		if !strings.Contains(filed, want) {
			t.Errorf("Comment() with tasks = %q, want it to hold %q", filed, want)
		}
	}
}

func ledgerFile(t *testing.T) review.File {
	t.Helper()
	file, err := review.Parse([]byte(`{"findings": [
		{"axis": "quality", "severity": "minor", "title": "Q one", "who": "agent", "body": "b"},
		{"axis": "spec", "severity": "major", "title": "S one", "who": "agent", "body": "b"},
		{"axis": "spec", "severity": "minor", "title": "S two", "who": "human", "body": "b"}
	], "not_filed": [{"title": "Split", "summary": "Too big."}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestParseLedgerRoundTripsAComment(t *testing.T) {
	t.Parallel()
	file := ledgerFile(t)

	got, found, err := review.ParseLedger(review.Comment(file, 42, "abc123", map[int]int{0: 7}))

	if err != nil || !found || got.Sha != "abc123" || !reflect.DeepEqual(got.File, file) {
		t.Errorf("ParseLedger() = %+v, %v, %v; want the ledger of %+v at abc123", got, found, err, file)
	}
}

func TestParseLedgerTellsALedgerFromAnyOtherComment(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "smith stopped its run: **complete**", "<!-- other -->"} {
		if _, found, err := review.ParseLedger(body); found || err != nil {
			t.Errorf("ParseLedger(%q) = found %v, %v; want no ledger and no error", body, found, err)
		}
	}
}

func TestParseLedgerRefusesABlockItCannotRead(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"not JSON":    "review\n<!-- smith:review {nope -->\n",
		"not closed":  "review\n<!-- smith:review {\"sha\":\"a\",\"findings\":[]}\n",
		"no sha":      "<!-- smith:review {\"findings\":[]} -->",
		"bad finding": `<!-- smith:review {"sha":"a","findings":[{"axis":"x"}]} -->`,
	} {
		if _, found, err := review.ParseLedger(body); !found || err == nil {
			t.Errorf("ParseLedger(%s) = found %v, %v; want a ledger found with an error", name, found, err)
		}
	}
}

func TestPendingNamesTheTaskOfEachFiledFindingAndListsTheRestInFilingOrder(t *testing.T) {
	t.Parallel()
	l := review.Ledger{Sha: "abc", File: ledgerFile(t)}
	subs := []review.Sub{
		{Number: 50, Body: "unrelated"},
		{Number: 51, Body: review.TaskBody(l.File.Findings[1], "S1", 42)},
		{Number: 52, Body: review.TaskBody(l.File.Findings[0], "Q1", 99)},
	}

	tasks, unfiled := review.Pending(l, 42, subs)

	if want := map[int]int{1: 51}; !reflect.DeepEqual(tasks, want) {
		t.Errorf("Pending() tasks = %v, want %v", tasks, want)
	}
	if want := []int{2, 0}; !slices.Equal(unfiled, want) {
		t.Errorf("Pending() unfiled = %v, want %v (S2 then Q1)", unfiled, want)
	}
}
