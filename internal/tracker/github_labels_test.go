package tracker_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestStartLabelsTheTaskAsWorkedAndClearsItsFailedMarkInOneEdit(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working", "smith:failed"}}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Start(t.Context(), 12); err != nil {
		t.Fatalf("Start(12) error = %v", err)
	}

	if want := []string{"issue edit 12 --add-label smith:working --remove-label smith:failed"}; !slices.Equal(gh.ran, want) {
		t.Errorf("Start(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestStopRemovesTheWorkedLabelInOneEdit(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working"}}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Stop(t.Context(), 12); err != nil {
		t.Fatalf("Stop(12) error = %v", err)
	}

	if want := []string{"issue edit 12 --remove-label smith:working"}; !slices.Equal(gh.ran, want) {
		t.Errorf("Stop(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestStartCreatesAMissingLabelAndRetriesOnce(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:failed"}}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Start(t.Context(), 12); err != nil {
		t.Fatalf("Start(12) error = %v", err)
	}

	want := []string{
		"issue edit 12 --add-label smith:working --remove-label smith:failed",
		"label create smith:working",
		"issue edit 12 --add-label smith:working --remove-label smith:failed",
	}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("Start(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestStartReportsARefusedEdit(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{stderr: "HTTP 403: Resource not accessible by integration\n"}

	err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Start(t.Context(), 12)

	var unavailable *tracker.UnavailableError
	if !errors.As(err, &unavailable) || len(gh.ran) != 1 {
		t.Errorf("Start(12) error = %v after gh %q, want an UnavailableError after one edit", err, gh.ran)
	}
}

func TestStartRetriesAnEditOnlyOnce(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{lostLabels: true}

	err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Start(t.Context(), 12)

	if err == nil || len(gh.ran) != 3 {
		t.Errorf("Start(12) error = %v after gh %q, want an error after one create and one retry", err, gh.ran)
	}
}

func TestFailHandsTheTaskToAHumanInOneEdit(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working", "smith:failed", "ready-for-agent", "ready-for-human"}}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Fail(t.Context(), 12); err != nil {
		t.Fatalf("Fail(12) error = %v", err)
	}

	want := []string{"issue edit 12 --add-label ready-for-human --add-label smith:failed --remove-label smith:working --remove-label ready-for-agent"}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("Fail(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestFailCreatesEachMissingLabel(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working", "ready-for-agent"}}

	if err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Fail(t.Context(), 12); err != nil {
		t.Fatalf("Fail(12) error = %v", err)
	}

	edit := "issue edit 12 --add-label ready-for-human --add-label smith:failed --remove-label smith:working --remove-label ready-for-agent"
	want := []string{edit, "label create ready-for-human", edit, "label create smith:failed", edit}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("Fail(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestExecutionReportsATaskLabelledAsWorked(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		specView(42): `{"body":"","labels":[{"name":"spec"}],"number":42,"state":"OPEN","url":"https://github.com/o/r/issues/42","subIssues":{"nodes":[{"number":43,"state":"OPEN","url":"https://github.com/o/r/issues/43"},{"number":44,"state":"OPEN","url":"https://github.com/o/r/issues/44"}],"totalCount":2},"title":"Spec"}`,
		taskView(43): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"},{"name":"smith:working"}],"number":43,"state":"OPEN","title":"Task 43"}`,
		taskView(44): agentTask(44, "OPEN", `{"nodes":[],"totalCount":0}`),
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	want := []tracker.Task{
		{Number: 43, Title: "Task 43", Open: true, For: tracker.Agent, Working: true},
		{Number: 44, Title: "Task 44", Open: true, For: tracker.Agent},
	}
	if !reflect.DeepEqual(sansLabels(got).Tasks, want) {
		t.Errorf("Execution(42) tasks =\n%+v\nwant\n%+v", got.Tasks, want)
	}
}

func TestTaskReportsATaskLabelledAsWorked(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		taskView(80): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"},{"name":"smith:working"}],"number":80,"state":"OPEN","subIssues":{"nodes":[],"totalCount":0},"title":"Fix X","url":"https://github.com/o/r/issues/80"}`,
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Task(t.Context(), 80)
	if err != nil {
		t.Fatalf("Task(80) error = %v", err)
	}

	if want := (tracker.Task{Number: 80, Title: "Fix X", Open: true, For: tracker.Agent, Working: true}); !reflect.DeepEqual(sansLabels(got), want) {
		t.Errorf("Task(80) = %+v, want %+v", got, want)
	}
}

func TestExecutionReadsEachTasksPresetLabels(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		specView(42): `{"body":"","labels":[{"name":"spec"}],"number":42,"state":"OPEN","url":"https://github.com/o/r/issues/42","subIssues":{"nodes":[{"number":43,"state":"OPEN","url":"https://github.com/o/r/issues/43"},{"number":44,"state":"OPEN","url":"https://github.com/o/r/issues/44"}],"totalCount":2},"title":"Spec"}`,
		taskView(43): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"},{"name":"preset:opus"},{"name":"preset:codex"}],"number":43,"state":"OPEN","title":"Task 43"}`,
		taskView(44): agentTask(44, "OPEN", `{"nodes":[],"totalCount":0}`),
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	want := []tracker.Task{
		{Number: 43, Title: "Task 43", Open: true, For: tracker.Agent, Presets: []string{"opus", "codex"}},
		{Number: 44, Title: "Task 44", Open: true, For: tracker.Agent},
	}
	if !reflect.DeepEqual(sansLabels(got).Tasks, want) {
		t.Errorf("Execution(42) tasks =\n%+v\nwant\n%+v", got.Tasks, want)
	}
}

func TestTaskReadsItsPresetLabel(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		taskView(80): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"},{"name":"preset:codex"}],"number":80,"state":"OPEN","subIssues":{"nodes":[],"totalCount":0},"title":"Fix X","url":"https://github.com/o/r/issues/80"}`,
	}}

	got, err := tracker.NewGitHub(gh, tracker.DefaultLabels()).Task(t.Context(), 80)
	if err != nil {
		t.Fatalf("Task(80) error = %v", err)
	}

	if want := (tracker.Task{Number: 80, Title: "Fix X", Open: true, For: tracker.Agent, Presets: []string{"codex"}}); !reflect.DeepEqual(sansLabels(got), want) {
		t.Errorf("Task(80) = %+v, want %+v", got, want)
	}
}

// configured are the triage labels a repo file can name in place of the defaults.
var configured = tracker.Labels{Agent: "go", Human: "stop", Spec: "epic"}

func TestExecutionReadsTasksByTheConfiguredLabels(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		specView(42): `{"body":"","labels":[],"number":42,"state":"OPEN","url":"https://github.com/o/r/issues/42","subIssues":{"nodes":[{"number":43,"state":"OPEN"},{"number":44,"state":"OPEN"},{"number":45,"state":"OPEN"}],"totalCount":3},"title":"Spec: labels"}`,
		taskView(43): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"go"}],"number":43,"state":"OPEN","title":"Task 43"}`,
		taskView(44): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"stop"}],"number":44,"state":"OPEN","title":"Task 44"}`,
		taskView(45): `{"blockedBy":{"nodes":[],"totalCount":0},"body":"","labels":[{"name":"ready-for-agent"}],"number":45,"state":"OPEN","title":"Task 45"}`,
	}}

	got, err := tracker.NewGitHub(gh, configured).Execution(t.Context(), 42)
	if err != nil {
		t.Fatalf("Execution(42) error = %v", err)
	}

	want := tracker.Execution{Number: 42, Title: "Spec: labels", Tasks: []tracker.Task{
		{Number: 43, Title: "Task 43", Open: true, For: tracker.Agent},
		{Number: 44, Title: "Task 44", Open: true, For: tracker.Human},
		{Number: 45, Title: "Task 45", Open: true, For: tracker.Nobody},
	}}
	if !reflect.DeepEqual(sansLabels(got), want) {
		t.Errorf("Execution(42) =\n%+v\nwant\n%+v", got, want)
	}
}

func TestExecutionTakesAnIssueWithTheConfiguredSpecLabelForASpec(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{replies: map[string]string{
		specView(76): `{"body":"","labels":[{"name":"epic"},{"name":"go"}],"number":76,"state":"OPEN","url":"https://github.com/o/r/issues/76","subIssues":{"nodes":[],"totalCount":0},"title":"Gadgets"}`,
	}}

	got, err := tracker.NewGitHub(gh, configured).Execution(t.Context(), 76)
	if err != nil {
		t.Fatalf("Execution(76) error = %v", err)
	}

	want := tracker.Execution{Number: 76, Title: "Gadgets", Tasks: []tracker.Task{}}
	if !reflect.DeepEqual(sansLabels(got), want) {
		t.Errorf("Execution(76) = %+v, want %+v", got, want)
	}
}

func TestFailHandsTheTaskFromTheConfiguredAgentLabelToTheConfiguredHumanOne(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working", "smith:failed", "go", "stop"}}

	if err := tracker.NewGitHub(gh, configured).Fail(t.Context(), 12); err != nil {
		t.Fatalf("Fail(12) error = %v", err)
	}

	want := []string{"issue edit 12 --add-label stop --add-label smith:failed --remove-label smith:working --remove-label go"}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("Fail(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestFailCreatesAMissingConfiguredHumanLabel(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"smith:working", "smith:failed", "go"}}

	if err := tracker.NewGitHub(gh, configured).Fail(t.Context(), 12); err != nil {
		t.Fatalf("Fail(12) error = %v", err)
	}

	edit := "issue edit 12 --add-label stop --add-label smith:failed --remove-label smith:working --remove-label go"
	want := []string{edit, "label create stop", edit}
	if !slices.Equal(gh.ran, want) {
		t.Errorf("Fail(12) ran gh %q, want %q", gh.ran, want)
	}
}

func TestLabelsListsTheLabelsTheRepoHas(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{labels: []string{"go", "smith:working", "ready-for-human"}}

	got, err := tracker.NewGitHub(gh, configured).Labels(t.Context())
	if err != nil {
		t.Fatalf("Labels() error = %v", err)
	}

	want := []string{"go", "smith:working", "ready-for-human"}
	if !slices.Equal(got, want) {
		t.Errorf("Labels() = %q, want %q", got, want)
	}
}

func TestLabelsReportsAnUnusableGH(t *testing.T) {
	t.Parallel()
	gh := &fakeGH{stderr: "HTTP 502: Bad Gateway\n"}

	_, err := tracker.NewGitHub(gh, configured).Labels(t.Context())

	var unavailable *tracker.UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Detail != "HTTP 502: Bad Gateway\n" {
		t.Errorf("Labels() error = %v, want an *tracker.UnavailableError carrying what gh said", err)
	}
}
