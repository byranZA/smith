package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "ts.key")
	if err := os.WriteFile(keyFile, []byte("tskey-from-file\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	tests := []struct {
		name    string
		ref     string
		env     map[string]string
		want    string
		wantErr error
	}{
		{
			name: "env scheme reads the variable",
			ref:  "env:TS_KEY",
			env:  map[string]string{"TS_KEY": "tskey-from-env"},
			want: "tskey-from-env",
		},
		{
			name: "file scheme reads the file",
			ref:  "file:" + keyFile,
			want: "tskey-from-file",
		},
		{
			name: "file scheme keeps a Windows drive path after the first colon",
			ref:  `file:C:\keys\ts`,
			// The arg is the Windows path; the file does not exist here so we only
			// assert that the split kept the drive letter, via the error path below.
			wantErr: os.ErrNotExist,
		},
		{
			name:    "bare literal is refused",
			ref:     "tskey-abc123",
			wantErr: ErrBareLiteral,
		},
		{
			name:    "empty reference is refused",
			ref:     "",
			wantErr: ErrBareLiteral,
		},
		{
			name:    "unknown scheme is refused",
			ref:     "vault:secret/ts",
			wantErr: ErrUnknownScheme,
		},
		{
			name:    "env with an unset variable errors",
			ref:     "env:DEFINITELY_UNSET_TS_KEY",
			wantErr: ErrNotFound,
		},
		{
			name:    "env with an empty variable name errors",
			ref:     "env:",
			wantErr: ErrEmptyArg,
		},
		{
			name:    "file with an empty path errors",
			ref:     "file:",
			wantErr: ErrEmptyArg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv panics under t.Parallel(), so only the env-free cases parallelize.
			if len(tt.env) == 0 {
				t.Parallel()
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := Resolve(tt.ref)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Resolve(%q) error = %v, want %v", tt.ref, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.ref, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestResolveTrimsWhitespace(t *testing.T) {
	t.Run("env value is trimmed", func(t *testing.T) {
		t.Setenv("TS_KEY", "  tskey-padded \n")
		got, err := Resolve("env:TS_KEY")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != "tskey-padded" {
			t.Errorf("Resolve = %q, want %q", got, "tskey-padded")
		}
	})

	t.Run("file value is trimmed", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "ts.key")
		if err := os.WriteFile(path, []byte("\ttskey-padded\r\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := Resolve("file:" + path)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != "tskey-padded" {
			t.Errorf("Resolve = %q, want %q", got, "tskey-padded")
		}
	})
}

func TestSplitCutsOnTheFirstColonOnly(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		wantScheme string
		wantArg    string
		wantOK     bool
	}{
		{"env reference", "env:GH_TOKEN", "env", "GH_TOKEN", true},
		{"windows path keeps its drive letter", `file:C:\keys\ts`, "file", `C:\keys\ts`, true},
		{"no scheme", "ghp_realtokenhere", "", "", false},
		{"empty argument", "env:", "env", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, arg, ok := Split(tt.ref)
			if ok != tt.wantOK {
				t.Fatalf("Split(%q) ok = %v, want %v", tt.ref, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if scheme != tt.wantScheme || arg != tt.wantArg {
				t.Errorf("Split(%q) = %q, %q, want %q, %q", tt.ref, scheme, arg, tt.wantScheme, tt.wantArg)
			}
		})
	}
}

func homeWithToken(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".secrets"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".secrets", "gh-token"), []byte("ghp_fromhome\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return home
}

func TestResolveFileExpandsAHomeRelativePath(t *testing.T) {
	homeWithToken(t)

	got, err := Resolve("file:~/.secrets/gh-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "ghp_fromhome" {
		t.Errorf("Resolve = %q, want %q", got, "ghp_fromhome")
	}
}

func TestResolveFileNamesAMissingHomeRelativePathAsWrittenAndExpanded(t *testing.T) {
	home := homeWithToken(t)

	_, err := Resolve("file:~/.secrets/missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve error = %v, want %v", err, os.ErrNotExist)
	}
	for _, want := range []string{"~/.secrets/missing", filepath.Join(home, ".secrets", "missing")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve error = %v, want it to name %q", err, want)
		}
	}
}

func TestResolveFileRefusesAnotherUsersHome(t *testing.T) {
	homeWithToken(t)

	_, err := Resolve("file:~other/token")
	if !errors.Is(err, ErrUserHome) {
		t.Fatalf("Resolve error = %v, want %v", err, ErrUserHome)
	}
}

func TestResolveFileReadsPathsThatAreNotHomeRelativeAsWritten(t *testing.T) {
	homeWithToken(t)
	work := t.TempDir()
	t.Chdir(work)
	if err := os.MkdirAll(filepath.Join(work, "relative"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "relative", "token"), []byte("from-work-dir"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "abs-token"), []byte("from-abs-path"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"an absolute path", filepath.Join(work, "abs-token"), "from-abs-path"},
		{"a relative path, from the working directory", "relative/token", "from-work-dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve("file:" + tt.path)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveFileReadsABareTildeAsTheHomeDirectoryItself(t *testing.T) {
	home := homeWithToken(t)

	_, err := Resolve("file:~")
	if err == nil {
		t.Fatal("Resolve error = nil, want reading the home directory to fail")
	}
	if !strings.Contains(err.Error(), home) {
		t.Errorf("Resolve error = %v, want it to name %q", err, home)
	}
}
