package tracker_test

import (
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestParseRefAcceptsEachWayOfNamingAnIssue(t *testing.T) {
	for _, ref := range []string{"42", "#42", "https://github.com/byranZA/smith/issues/42", "https://github.com/byranZA/smith/issues/42/", " 42 "} {
		t.Run(ref, func(t *testing.T) {
			got, err := tracker.ParseRef(ref)
			if err != nil {
				t.Fatalf("ParseRef(%q) error = %v", ref, err)
			}
			if got != 42 {
				t.Errorf("ParseRef(%q) = %d, want 42", ref, got)
			}
		})
	}
}

func TestParseRefRefusesWhatIsNotAnIssue(t *testing.T) {
	for _, ref := range []string{"", "#", "forty-two", "0", "-3", "https://github.com/byranZA/smith/pull/42", "42abc"} {
		t.Run(ref, func(t *testing.T) {
			if got, err := tracker.ParseRef(ref); err == nil {
				t.Errorf("ParseRef(%q) = %d, want an error", ref, got)
			}
		})
	}
}
