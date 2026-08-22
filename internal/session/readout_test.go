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
