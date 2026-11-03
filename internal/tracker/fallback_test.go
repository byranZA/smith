package tracker_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

// partOfList is the key of the search for the issues whose body says they are
// part of issue n, the way a gh without --parent leaves them.
func partOfList(n int) string {
	return fmt.Sprintf("issue list --state all --limit 1000 --search \"Part of #%d\" in:body --json number,body", n)
}

// viewOf is issue n as a spec view of an issue with no native links.
func plainView(n int, labels, body string) string {
	return fmt.Sprintf(`{"body":%q,"labels":[%s],"number":%d,"state":"OPEN","url":"https://github.com/o/r/issues/%d","subIssues":{"nodes":[],"totalCount":0},"title":"Issue %d"}`, body, labels, n, n, n)
}

func filedTask(n int, body string) string {
	return fmt.Sprintf(`{"blockedBy":{"nodes":[],"totalCount":0},"body":%q,"labels":[{"name":"ready-for-agent"}],"number":%d,"state":"OPEN","title":"Task %d"}`, body, n, n)
}

func TestExecutionWorksTasksFiledWithPartOfLinesUnderTheirSpec(t *testing.T) {
	t.Parallel()
	first, second := "Part of #42\n\nFirst", "Part of #42\nBlocked by #101\n\nSecond"
	gh := &fakeGH{replies: map[string]string{
		specView(42):   plainView(42, `{"name":"spec"}`, ""),
		partOfList(42): fmt.Sprintf(`[{"number":101,"body":%q},{"number":102,"body":%q},{"number":103,"body":"Part of #420\n\nOther"}]`, first, second),
		taskView(101):  filedTask(101, first),
		taskView(102):  filedTask(102, second),
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	want := tracker.Execution{Number: 42, Title: "Issue 42", Tasks: []tracker.Task{
		{Number: 101, Title: "Task 101", Open: true, For: tracker.Agent, Body: first},
		{Number: 102, Title: "Task 102", Open: true, For: tracker.Agent, Blockers: []tracker.Blocker{{Number: 101, Open: true}}, Body: second},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Execution(42) =\n%+v\nwant\n%+v", got, want)
	}
}

func TestExecutionOfAStandaloneTaskWithPartOfChildrenIsThoseChildren(t *testing.T) {
	t.Parallel()
	child := "Part of #80\n\nFinding"
	gh := &fakeGH{replies: map[string]string{
		specView(80):   plainView(80, `{"name":"ready-for-agent"}`, "Fix X"),
		partOfList(80): fmt.Sprintf(`[{"number":81,"body":%q}]`, child),
		taskView(81):   filedTask(81, child),
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 80)
	if err != nil {
		t.Fatalf("Execution(80) error = %v", err)
	}

	want := tracker.Execution{Number: 80, Title: "Issue 80", Tasks: []tracker.Task{
		{Number: 81, Title: "Task 81", Open: true, For: tracker.Agent, Body: child},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Execution(80) =\n%+v\nwant\n%+v", got, want)
	}
}

func TestExecutionOfATaskFiledWithAPartOfLineIsTheTaskAloneUnderItsParent(t *testing.T) {
	t.Parallel()
	body := "Part of #42\n\nFinding"
	gh := &fakeGH{replies: map[string]string{
		specView(101):                       plainView(101, `{"name":"ready-for-agent"}`, body),
		taskView(101):                       filedTask(101, body),
		"issue view 42 --json number,title": `{"number":42,"title":"Spec: the loop"}`,
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 101)
	if err != nil {
		t.Fatalf("Execution(101) error = %v", err)
	}

	want := tracker.Execution{Number: 42, Title: "Spec: the loop", Single: true, Tasks: []tracker.Task{
		{Number: 101, Title: "Task 101", Open: true, For: tracker.Agent},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Execution(101) =\n%+v\nwant\n%+v", got, want)
	}
}
