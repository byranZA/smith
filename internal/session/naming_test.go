package session_test

import (
	"testing"

	"github.com/byranZA/smith/internal/session"
)

func TestDeriveName(t *testing.T) {
	tests := []struct {
		name   string
		repo   string
		branch string
		want   string
	}{
		{"a simple branch derives a simple name", "smith", "main", "smith-main"},
		{"a slashed branch sanitizes to one path segment", "smith", "smith/spec-42", "smith-smith-spec-42"},
		{"dots, dashes and underscores survive", "web", "release-2.1_rc", "web-release-2.1_rc"},
		{"spaces and punctuation become dashes", "web", "fix (#42)", "web-fix---42-"},
		{"a slashed repo sanitizes too", "acme/web", "main", "acme-web-main"},
		{"a branch of only separators still yields one segment", "web", "//", "web---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := session.DeriveName(tt.repo, tt.branch); got != tt.want {
				t.Errorf("DeriveName(%q, %q) = %q, want %q", tt.repo, tt.branch, got, tt.want)
			}
		})
	}
}

func TestTmuxSession(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a session name is namespaced under smith", "smith-main", "smith/smith-main"},
		{"a sanitized name is namespaced unchanged", "smith-smith-spec-42", "smith/smith-smith-spec-42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := session.TmuxSession(tt.in); got != tt.want {
				t.Errorf("TmuxSession(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
