package tracker_test

import (
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestParseRefAcceptsEachWayOfNamingAnIssue(t *testing.T) {
	t.Parallel()
	for ref, want := range map[string]tracker.Ref{
		"42":  {Number: 42},
		"#42": {Number: 42},
		"https://github.com/byranZA/smith/issues/42":  {Repo: "byranZA/smith", Number: 42},
		"https://github.com/byranZA/smith/issues/42/": {Repo: "byranZA/smith", Number: 42},
		" 42 ": {Number: 42},
	} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			got, err := tracker.ParseRef(ref)
			if err != nil {
				t.Fatalf("ParseRef(%q) error = %v", ref, err)
			}
			if got != want {
				t.Errorf("ParseRef(%q) = %v, want %v", ref, got, want)
			}
		})
	}
}

func TestParseRefRefusesWhatIsNotAnIssue(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"", "#", "forty-two", "0", "-3", "https://github.com/byranZA/smith/pull/42", "42abc", "99999999999999999999"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			if got, err := tracker.ParseRef(ref); err == nil {
				t.Errorf("ParseRef(%q) = %v, want an error", ref, got)
			}
		})
	}
}

func TestParseRefKeepsTheRepoAURLNames(t *testing.T) {
	t.Parallel()
	for ref, want := range map[string]tracker.Ref{
		"42":  {Number: 42},
		"#42": {Number: 42},
		"https://github.com/byranZA/smith/issues/42": {Repo: "byranZA/smith", Number: 42},
		"https://github.com/other/repo/issues/42":    {Repo: "other/repo", Number: 42},
	} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			got, err := tracker.ParseRef(ref)
			if err != nil || got != want {
				t.Errorf("ParseRef(%q) = %v, %v; want %v", ref, got, err, want)
			}
		})
	}
}
