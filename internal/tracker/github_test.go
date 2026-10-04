package tracker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// fakeGH replays recorded `gh` output keyed by the arguments it was run with,
// failing the way gh does for an issue it has no answer for.
type fakeGH struct {
	replies map[string]string
	pages   map[string]string
	stderr  string
	launch  error
	ran     []string
}

func (f *fakeGH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if f.launch != nil {
		return f.launch
	}
	f.ran = append(f.ran, strings.Join(args, " "))
	if name != "gh" {
		return fmt.Errorf("unexpected command %s", name)
	}
	if f.stderr != "" {
		if _, err := io.WriteString(stderr, f.stderr); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	if args[0] == "api" {
		return f.page(args, stdout, stderr)
	}
	reply, ok := f.replies[strings.Join(args, " ")]
	if !ok {
		if _, err := io.WriteString(stderr, "GraphQL: Could not resolve to an issue or pull request with the number of "+args[2]+". (repository.issue)\n"); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	_, err := io.WriteString(stdout, reply)
	return err
}

// page replays a recorded GraphQL page of an issue's linked issues, keyed by
// pageKey, failing the way gh does when GitHub cannot answer.
func (f *fakeGH) page(args []string, stdout, stderr io.Writer) error {
	fields := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-F" || args[i] == "-f" {
			name, value, _ := strings.Cut(args[i+1], "=")
			fields[name] = value
		}
	}
	connection := "blockedBy"
	if strings.Contains(fields["query"], "subIssues(") {
		connection = "subIssues"
	}
	reply, ok := f.pages[pageKey(connection, fields["number"], fields["endCursor"])]
	if !ok {
		if _, err := io.WriteString(stderr, "HTTP 502: Bad Gateway (https://api.github.com/graphql)\n"); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	_, err := io.WriteString(stdout, reply)
	return err
}

func pageKey(connection, number, cursor string) string {
	return connection + " " + number + " after " + cursor
}

// graphQLPage is GitHub's answer for one page of an issue's linked issues.
func graphQLPage(connection string, nodes []string, next string) string {
	return fmt.Sprintf(`{"data":{"repository":{"issue":{%q:{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}}`,
		connection, strings.Join(nodes, ","), next != "", next)
}

func node(n int, state string) string {
	return fmt.Sprintf(`{"number":%d,"state":%q}`, n, state)
}

func agentTask(n int, state, blockedBy string) string {
	return fmt.Sprintf(`{"blockedBy":%s,"body":"","labels":[{"name":"ready-for-agent"}],"number":%d,"state":%q,"title":"Task %d"}`, blockedBy, n, state, n)
}

// manyChildren is gh's output for spec #42 with 101 children: #101 to #200
// closed and #201 open, which gh's issue view cuts off after the first 100.
// The spec's checklist lists #201 then #150.
func manyChildren() *fakeGH {
	gh := &fakeGH{replies: map[string]string{}, pages: map[string]string{}}
	var first, second []string
	for n := 101; n <= 200; n++ {
		first = append(first, node(n, "CLOSED"))
		gh.replies[taskView(n)] = agentTask(n, "CLOSED", `{"nodes":[],"totalCount":0}`)
	}
	second = append(second, node(201, "OPEN"))
	gh.replies[taskView(201)] = agentTask(201, "OPEN", `{"nodes":[],"totalCount":0}`)
	gh.replies[specView(42)] = fmt.Sprintf(`{"body":"## Tasks\n\n- [ ] #201\n- [ ] #150\n","labels":[{"name":"spec"}],"number":42,"state":"OPEN","subIssues":{"nodes":[%s],"totalCount":101},"title":"Spec: many"}`, strings.Join(first, ","))
	gh.pages[pageKey("subIssues", "42", "")] = graphQLPage("subIssues", first[:60], "c60")
	gh.pages[pageKey("subIssues", "42", "c60")] = graphQLPage("subIssues", first[60:], "c100")
	gh.pages[pageKey("subIssues", "42", "c100")] = graphQLPage("subIssues", second, "")
	return gh
}

const (
	specFields = "number,title,state,labels,body,subIssues"
	taskFields = "number,title,state,labels,body,blockedBy"
)

func specView(n int) string { return fmt.Sprintf("issue view %d --json %s", n, specFields) }
func taskView(n int) string { return fmt.Sprintf("issue view %d --json %s", n, taskFields) }

// recorded is gh's output for spec #42, whose checklist lists #45 before #43,
// with #44 left off it and #50 filed as a child after the spec was written.
func recorded() map[string]string {
	return map[string]string{
		specView(42):                       `{"body":"## Problem\n\nSee #7.\n\n## Tasks\n\n- [ ] #45\n- [ ] #43\n","labels":[{"id":"LA_1","name":"spec","description":"Feature spec / PRD","color":"0E8A16"}],"number":42,"state":"OPEN","subIssues":{"nodes":[{"id":"I_43","number":43,"state":"CLOSED","title":"The tracker","url":"https://github.com/o/r/issues/43"},{"id":"I_44","number":44,"state":"OPEN","title":"The prompt","url":"https://github.com/o/r/issues/44"},{"id":"I_45","number":45,"state":"OPEN","title":"The repo file","url":"https://github.com/o/r/issues/45"},{"id":"I_50","number":50,"state":"OPEN","title":"Fix the summary","url":"https://github.com/o/r/issues/50"}],"totalCount":4},"title":"Spec: the loop"}`,
		taskView(43):                       `{"blockedBy":{"nodes":[],"totalCount":0},"body":"## What to build\n\nThe tracker.\n\n## Blocked by\n\nNone - can start immediately\n","labels":[{"id":"LA_2","name":"ready-for-agent","description":"","color":"000000"}],"number":43,"state":"CLOSED","title":"The tracker"}`,
		taskView(44):                       `{"blockedBy":{"nodes":[{"id":"I_45","number":45,"state":"OPEN","title":"The repo file","url":"https://github.com/o/r/issues/45"}],"totalCount":1},"body":"## Blocked by\n\n- #43\n- #7\n","labels":[{"id":"LA_3","name":"ready-for-human","description":"","color":"000000"}],"number":44,"state":"OPEN","title":"The prompt"}`,
		taskView(45):                       `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"id":"LA_2","name":"ready-for-agent","description":"","color":"000000"},{"id":"LA_3","name":"ready-for-human","description":"","color":"000000"}],"number":45,"state":"OPEN","title":"The repo file"}`,
		taskView(50):                       `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[],"number":50,"state":"OPEN","title":"Fix the summary"}`,
		"issue view 7 --json number,state": `{"number":7,"state":"CLOSED"}`,
	}
}

func TestSpecReadsTheSpecAndItsTasksInListedOrder(t *testing.T) {
	gh := &fakeGH{replies: recorded()}

	got, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}

	want := tracker.Spec{Number: 42, Title: "Spec: the loop", Tasks: []tracker.Task{
		{Number: 45, Title: "The repo file", Open: true, For: tracker.Human},
		{Number: 43, Title: "The tracker", Open: false, For: tracker.Agent},
		{Number: 44, Title: "The prompt", Open: true, For: tracker.Human, Blockers: []tracker.Blocker{{Number: 45, Open: true}, {Number: 43, Open: false}, {Number: 7, Open: false}}},
		{Number: 50, Title: "Fix the summary", Open: true, For: tracker.Nobody},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Spec() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSpecOnlyReadsTheTracker(t *testing.T) {
	gh := &fakeGH{replies: recorded()}

	if _, err := tracker.NewGitHub(gh).Spec(context.Background(), 42); err != nil {
		t.Fatalf("Spec() error = %v", err)
	}

	for _, ran := range gh.ran {
		if !strings.HasPrefix(ran, "issue view ") && !strings.HasPrefix(ran, "api graphql ") {
			t.Errorf("ran gh %s, want only issue views and queries", ran)
		}
	}
}

func TestSpecRefusesAnIssueThatIsNotASpec(t *testing.T) {
	gh := &fakeGH{replies: map[string]string{
		specView(43): `{"body":"","labels":[{"id":"LA_2","name":"ready-for-agent","description":"","color":"000000"}],"number":43,"state":"OPEN","subIssues":{"nodes":[],"totalCount":0},"title":"The tracker"}`,
	}}

	_, err := tracker.NewGitHub(gh).Spec(context.Background(), 43)

	var notSpec *tracker.NotSpecError
	if !errors.As(err, &notSpec) || notSpec.Number != 43 {
		t.Errorf("Spec() error = %v, want a NotSpecError for #43", err)
	}
}

func TestSpecReportsAMissingIssueAsNotFound(t *testing.T) {
	gh := &fakeGH{replies: map[string]string{}}

	_, err := tracker.NewGitHub(gh).Spec(context.Background(), 9999)

	var notFound *tracker.NotFoundError
	if !errors.As(err, &notFound) || notFound.Number != 9999 {
		t.Errorf("Spec() error = %v, want a NotFoundError for #9999", err)
	}
}

func TestSpecReportsAnUnusableGH(t *testing.T) {
	for name, gh := range map[string]*fakeGH{
		"not authenticated": {stderr: "To get started with GitHub CLI, please run:  gh auth login\n"},
		"not installed":     {launch: exec.ErrNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)

			var unavailable *tracker.UnavailableError
			if !errors.As(err, &unavailable) {
				t.Errorf("Spec() error = %v, want an UnavailableError", err)
			}
		})
	}
}

func TestUnavailableErrorCarriesWhatGHSaid(t *testing.T) {
	gh := &fakeGH{stderr: "To get started with GitHub CLI, please run:  gh auth login\n"}

	_, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)

	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Spec() error = %v, want it to carry gh's own message", err)
	}
}

func TestSpecReadsChildrenPastTheFirstHundred(t *testing.T) {
	gh := manyChildren()

	got, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}

	next, ok := loop.Survey(got).Next()
	if !ok || next.Number != 201 {
		t.Errorf("next task = #%d (%v), want #201", next.Number, ok)
	}
}

func TestSpecOrdersChildrenAcrossPages(t *testing.T) {
	gh := manyChildren()

	got, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}

	var order []int
	for _, task := range got.Tasks {
		order = append(order, task.Number)
	}
	want := []int{201, 150}
	for n := 101; n <= 200; n++ {
		if n != 150 {
			want = append(want, n)
		}
	}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("task order = %v, want %v", order, want)
	}
}

func TestSpecReadsBlockersPastTheFirstFifty(t *testing.T) {
	gh := &fakeGH{replies: map[string]string{}, pages: map[string]string{}}
	var first []string
	for n := 301; n <= 350; n++ {
		first = append(first, node(n, "CLOSED"))
	}
	gh.replies[specView(42)] = `{"body":"","labels":[{"name":"spec"}],"number":42,"state":"OPEN","subIssues":{"nodes":[{"number":43,"state":"OPEN"}],"totalCount":1},"title":"Spec"}`
	gh.replies[taskView(43)] = agentTask(43, "OPEN", fmt.Sprintf(`{"nodes":[%s],"totalCount":51}`, strings.Join(first, ",")))
	gh.pages[pageKey("blockedBy", "43", "")] = graphQLPage("blockedBy", first, "c50")
	gh.pages[pageKey("blockedBy", "43", "c50")] = graphQLPage("blockedBy", []string{node(351, "OPEN")}, "")

	got, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}

	if next, ok := loop.Survey(got).Next(); ok {
		t.Errorf("next task = #%d, want none while #351 blocks #43", next.Number)
	}
}

func TestSpecFailsWhenALaterPageCannotBeRead(t *testing.T) {
	gh := manyChildren()
	delete(gh.pages, pageKey("subIssues", "42", "c100"))

	got, err := tracker.NewGitHub(gh).Spec(context.Background(), 42)

	var unavailable *tracker.UnavailableError
	if !errors.As(err, &unavailable) || !reflect.DeepEqual(got, tracker.Spec{}) {
		t.Errorf("Spec() = %d tasks, %v; want no spec and an UnavailableError", len(got.Tasks), err)
	}
}
