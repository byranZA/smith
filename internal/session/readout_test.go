package session_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/session"
)

// TestReadoutCarriesTheFourColumnsAndNoOther locks in the table's shape: the
// header an operator scans, and nothing beside it.
func TestReadoutCarriesTheFourColumnsAndNoOther(t *testing.T) {
	out := session.Readout([]session.Session{
		{Name: "smith-main", Repo: "smith", Branch: "main", Live: true},
	})

	header := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	want := []string{"NAME", "STATE", "DIRTY", "UNPUSHED"}
	if strings.Join(header, " ") != strings.Join(want, " ") {
		t.Errorf("Readout() header = %v, want exactly %v", header, want)
	}
}

// TestReadoutNamesTheSessionTheOtherVerbsTake locks in that the identity
// column is the sanitized name and that the true branch appears nowhere: the
// column exists to be pasted into the next command.
func TestReadoutNamesTheSessionTheOtherVerbsTake(t *testing.T) {
	out := session.Readout([]session.Session{
		{Name: session.DeriveName("smith", "smith/spec-42"), Repo: "smith", Branch: "smith/spec-42"},
	})

	if !strings.Contains(out, "smith-smith-spec-42") {
		t.Errorf("Readout() = %q, want it to name the session", out)
	}
	if strings.Contains(out, "smith/spec-42") {
		t.Errorf("Readout() = %q, want the true branch left out of the default output", out)
	}
}

// TestReadoutStatesWhetherEachSessionIsRunning locks in the STATE column's two
// words, flat and in the order it was handed.
func TestReadoutStatesWhetherEachSessionIsRunning(t *testing.T) {
	out := session.Readout([]session.Session{
		{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Live: true},
		{Name: "web-hotfix", Repo: "web", Branch: "hotfix"},
	})

	rows := strings.Split(strings.TrimSpace(out), "\n")
	if len(rows) < 3 {
		t.Fatalf("Readout() = %q, want a header and two rows", out)
	}
	if got := strings.Fields(rows[1]); got[0] != "smith-spec-42" || got[1] != "live" {
		t.Errorf("Readout() first row = %v, want the live session first", got)
	}
	if got := strings.Fields(rows[2]); got[0] != "web-hotfix" || got[1] != "stopped" {
		t.Errorf("Readout() second row = %v, want the stopped session second", got)
	}
}

// TestReadoutAlwaysPrintsTheSummary locks in the line an operator reads before
// tearing a box down by hand: unconditional, because a line that can be absent
// cannot reassure.
func TestReadoutAlwaysPrintsTheSummary(t *testing.T) {
	tests := []struct {
		name     string
		sessions []session.Session
		want     string
	}{
		{"no sessions", nil, "no sessions"},
		{"one session", []session.Session{{Name: "smith-main"}}, "1 session"},
		{"three sessions", []session.Session{{Name: "a"}, {Name: "b"}, {Name: "c"}}, "3 sessions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := session.Readout(tt.sessions)
			if !strings.Contains(out, tt.want) {
				t.Errorf("Readout() = %q, want a summary reading %q", out, tt.want)
			}
		})
	}
}

// TestReadoutRendersTheWorkState locks in what the two work-state columns say:
// dirty is a word or a dash, unpushed is a count, and a detached worktree —
// which has no branch to count — is a dash rather than a zero.
func TestReadoutRendersTheWorkState(t *testing.T) {
	tests := []struct {
		name            string
		session         session.Session
		dirty, unpushed string
	}{
		{"clean and pushed", session.Session{Name: "a", Branch: "spec-42"}, "-", "0"},
		{"dirty", session.Session{Name: "a", Branch: "spec-42", Dirty: true}, "dirty", "0"},
		{"unpushed commits", session.Session{Name: "a", Branch: "spec-42", Unpushed: 3}, "-", "3"},
		{"detached", session.Session{Name: "a"}, "-", "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := strings.Fields(strings.Split(session.Readout([]session.Session{tt.session}), "\n")[1])

			if len(row) != 4 {
				t.Fatalf("Readout() row = %v, want four columns", row)
			}
			if row[2] != tt.dirty || row[3] != tt.unpushed {
				t.Errorf("Readout() dirty, unpushed = %q, %q, want %q, %q", row[2], row[3], tt.dirty, tt.unpushed)
			}
		})
	}
}

// TestReadoutSummarizesTheWorkState locks in the line an operator reads before
// tearing a box down by hand: what is at risk, or that nothing is.
func TestReadoutSummarizesTheWorkState(t *testing.T) {
	tests := []struct {
		name     string
		sessions []session.Session
		want     string
	}{
		{
			"one dirty and one unpushed",
			[]session.Session{
				{Name: "a", Branch: "a", Dirty: true},
				{Name: "b", Branch: "b", Unpushed: 2},
				{Name: "c", Branch: "c"},
			},
			"3 sessions · 1 dirty · 1 with unpushed commits",
		},
		{
			"everything clean",
			[]session.Session{{Name: "a", Branch: "a"}, {Name: "b", Branch: "b"}, {Name: "c", Branch: "c"}},
			"3 sessions · all clean",
		},
		{
			"only dirty work",
			[]session.Session{{Name: "a", Branch: "a", Dirty: true}, {Name: "b", Branch: "b"}},
			"2 sessions · 1 dirty",
		},
		{
			"a detached worktree counts nothing unpushed",
			[]session.Session{{Name: "a"}},
			"1 session · all clean",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.TrimSpace(session.Readout(tt.sessions)); !strings.HasSuffix(got, tt.want) {
				t.Errorf("Readout() = %q, want a summary reading %q", got, tt.want)
			}
		})
	}
}

// TestNamesPrintsBareNamesOnePerLine locks in the one output that is not for a
// human to read: no header, no columns and no summary, so it composes straight
// into a batch removal.
func TestNamesPrintsBareNamesOnePerLine(t *testing.T) {
	got := session.Names([]session.Session{
		{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Unpushed: 2},
		{Name: "web-hotfix", Repo: "web", Branch: "hotfix", Dirty: true},
	})

	if got != "smith-spec-42\nweb-hotfix\n" {
		t.Errorf("Names() = %q, want one bare name per line", got)
	}
}

// TestNamesPrintsNothingWhenThereIsNothingToList locks in that an empty
// listing composes into a removal of nothing rather than into a word.
func TestNamesPrintsNothingWhenThereIsNothingToList(t *testing.T) {
	if got := session.Names(nil); got != "" {
		t.Errorf("Names(nil) = %q, want nothing at all", got)
	}
}
