package loop_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/repofile"
)

// fakeLabels is a repo holding labels, or failing to list them with err.
type fakeLabels struct {
	labels []string
	err    error
	calls  int
}

func (f *fakeLabels) Labels(context.Context) ([]string, error) {
	f.calls++
	return f.labels, f.err
}

func settingsWith(t *testing.T, labels repofile.Labels) loop.Settings {
	t.Helper()
	s, err := loop.ResolveSettings(repofile.File{}, repofile.File{Labels: labels})
	if err != nil {
		t.Fatalf("ResolveSettings(labels %+v) error = %v", labels, err)
	}
	return s
}

func TestLabelWarningsNamesEachSetLabelTheRepoLacks(t *testing.T) {
	t.Parallel()
	repo := &fakeLabels{labels: []string{"ready-for-agent", "smith:working"}}
	s := settingsWith(t, repofile.Labels{Agent: "go", Human: "stop", Spec: "epic"})

	got := loop.LabelWarnings(t.Context(), repo, s)

	want := []string{
		`labels.agent is "go", a label this repo does not have: no issue will match it, and smith never creates it`,
		`labels.human is "stop", a label this repo does not have: smith will create it the first time it skips a task`,
		`labels.spec is "epic", a label this repo does not have: no issue will match it, and smith never creates it`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("LabelWarnings() = %q, want %q", got, want)
	}
}

func TestLabelWarningsPassesOverASetLabelTheRepoHas(t *testing.T) {
	t.Parallel()
	repo := &fakeLabels{labels: []string{"go", "stop"}}
	s := settingsWith(t, repofile.Labels{Agent: "go", Human: "stop"})

	if got := loop.LabelWarnings(t.Context(), repo, s); len(got) != 0 {
		t.Errorf("LabelWarnings() = %q, want none", got)
	}
}

func TestLabelWarningsNeverChecksDefaultLabels(t *testing.T) {
	t.Parallel()
	repo := &fakeLabels{}
	s := settingsWith(t, repofile.Labels{})

	got := loop.LabelWarnings(t.Context(), repo, s)

	if len(got) != 0 || repo.calls != 0 {
		t.Errorf("LabelWarnings() = %q after %d label reads, want none and no read", got, repo.calls)
	}
}

func TestLabelWarningsChecksOnlyTheLabelsSet(t *testing.T) {
	t.Parallel()
	repo := &fakeLabels{}
	s := settingsWith(t, repofile.Labels{Human: "stop"})

	got := loop.LabelWarnings(t.Context(), repo, s)

	want := []string{`labels.human is "stop", a label this repo does not have: smith will create it the first time it skips a task`}
	if !slices.Equal(got, want) {
		t.Errorf("LabelWarnings() = %q, want %q", got, want)
	}
}

func TestLabelWarningsSaysSoWhenTheCheckCannotRun(t *testing.T) {
	t.Parallel()
	repo := &fakeLabels{err: errors.New("gh could not be used: HTTP 401")}
	s := settingsWith(t, repofile.Labels{Agent: "go"})

	got := loop.LabelWarnings(t.Context(), repo, s)

	want := []string{"could not check the repo has the labels the repo file sets: gh could not be used: HTTP 401"}
	if !slices.Equal(got, want) {
		t.Errorf("LabelWarnings() = %q, want %q", got, want)
	}
}
