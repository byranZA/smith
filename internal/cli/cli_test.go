package cli

import (
	"bytes"
	"errors"
	"testing"
)

func TestSetupRejectsInvalidAccessMode(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"machine", "setup", "root@box", "--access", "vpn"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want an error rejecting the unknown access mode")
	}
	if code := codeFromError(err); code == 0 {
		t.Errorf("exit code = 0, want non-zero for an invalid --access value")
	}
}

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

func TestParseStatusHost(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare host", "box", "box", false},
		{"tailnet name", "smith-box", "smith-box", false},
		{"login@host rejected", "root@box", "", true},
		{"empty rejected", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatusHost(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseStatusHost(%q) err = nil, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStatusHost(%q) err = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseStatusHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestModuleVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tagged release strips the v", "v0.1.0", "0.1.0"},
		{"prerelease tag strips the v", "v0.1.0-rc.1", "0.1.0-rc.1"},
		{"already bare passes through", "0.2.0", "0.2.0"},
		{"local devel is not a version", "(devel)", ""},
		{"empty is not a version", "", ""},
		{"pseudo-version is not a release", "v0.0.0-20260728155309-f8e683ead820", ""},
		{"dirty pseudo-version is not a release", "v0.0.0-20260728155309-f8e683ead820+dirty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleVersion(tt.in); got != tt.want {
				t.Errorf("moduleVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"full revision trims to seven", "ef7c059abc1234def", "ef7c059"},
		{"short revision untouched", "ef7c05", "ef7c05"},
		{"empty untouched", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortCommit(tt.in); got != tt.want {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.in, got, tt.want)
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
