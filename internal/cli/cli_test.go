package cli

import (
	"errors"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"root login", "root@box", "root@box", false},
		{"sudo login", "ubuntu@10.0.0.1", "ubuntu@10.0.0.1", false},
		{"missing login", "box", "", true},
		{"empty login", "@box", "", true},
		{"empty host", "root@", "", true},
		{"empty string", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTarget(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTarget(%q) err = nil, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget(%q) err = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseTarget(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, 0},
		{"exit error carries its code", &exitError{code: 2}, 2},
		{"connect exit code", &exitError{code: 3}, 3},
		{"plain error is general failure", errors.New("boom"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codeFromError(tt.err); got != tt.want {
				t.Errorf("codeFromError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
