package workspace

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
)

// convergeProfile runs the stage over what a blueprint declares against a box
// and a home of the test's own, writing the login profile at profile.
func convergeProfile(t *testing.T, box *fakeBox, home, profile string, b blueprint.Blueprint) Result {
	t.Helper()
	root := t.TempDir()
	stageEnv(t, root, b)
	env := Env{Command: box, StateRoot: root, Profile: profile}
	result, err := Converge(context.Background(), env, Plan(b, resolved, home), io.Discard)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result
}

// loginPath is the PATH a login shell holds after sourcing profile, with home
// as its HOME and path as the PATH it started with.
func loginPath(t *testing.T, profile, home, path string) string {
	t.Helper()
	var out, errOut strings.Builder
	args := []string{"-i", "HOME=" + home, "PATH=" + path, "/bin/sh", "-c", `. "$1"; printf %s "$PATH"`, "sh", profile}
	if err := connection.System().Run(t.Context(), "/usr/bin/env", args, nil, &out, &errOut); err != nil {
		t.Fatalf("source %s in a login shell: %v (%s)", profile, err, errOut.String())
	}
	return out.String()
}

// shims makes the mise shims directory under home and returns it.
func shims(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".local", "share", "mise", "shims")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make the shims directory: %v", err)
	}
	return dir
}

func TestConvergeWritesAProfilePuttingTheShimsFirstOnPath(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(t.TempDir(), "smith.sh")
	box := &fakeBox{installed: map[string]bool{}, hasMise: true}

	result := convergeProfile(t, box, home, profile, blueprint.Blueprint{Tools: map[string]string{"gh": "2.60"}})

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	want := shims(t, home) + ":/usr/bin:/bin"
	if got := loginPath(t, profile, home, "/usr/bin:/bin"); got != want {
		t.Errorf("PATH after sourcing %s = %q, want %q", profile, got, want)
	}
}

func TestConvergeLeavesThePathAloneOnABoxWithNoShims(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(t.TempDir(), "smith.sh")
	box := &fakeBox{installed: map[string]bool{}, hasMise: true}

	convergeProfile(t, box, home, profile, blueprint.Blueprint{Tools: map[string]string{"gh": "2.60"}})

	if got := loginPath(t, profile, home, "/usr/bin:/bin"); got != "/usr/bin:/bin" {
		t.Errorf("PATH after sourcing %s = %q, want %q", profile, got, "/usr/bin:/bin")
	}
}

func TestConvergeLeavesAnUnchangedProfileAlone(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(t.TempDir(), "smith.sh")
	b := blueprint.Blueprint{Tools: map[string]string{"gh": "2.60"}}
	convergeProfile(t, &fakeBox{installed: map[string]bool{}, hasMise: true}, home, profile, b)
	box := &fakeBox{installed: map[string]bool{}, hasMise: true}

	result := convergeProfile(t, box, home, profile, b)

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if box.ran("sudo install") {
		t.Errorf("the stage ran %v, want an unchanged profile left alone", box.calls)
	}
}

func TestConvergeRewritesAProfileThatDiffers(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(t.TempDir(), "smith.sh")
	if err := os.WriteFile(profile, []byte("PATH=/stale:$PATH\n"), 0o644); err != nil {
		t.Fatalf("write a stale profile: %v", err)
	}
	box := &fakeBox{installed: map[string]bool{}, hasMise: true}

	convergeProfile(t, box, home, profile, blueprint.Blueprint{Tools: map[string]string{"gh": "2.60"}})

	want := shims(t, home) + ":/usr/bin:/bin"
	if got := loginPath(t, profile, home, "/usr/bin:/bin"); got != want {
		t.Errorf("PATH after sourcing %s = %q, want %q", profile, got, want)
	}
}

func TestConvergeWritesNoProfileOnABoxWithNoToolchain(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(t.TempDir(), "smith.sh")
	box := newBox()

	convergeProfile(t, box, home, profile, blueprint.Blueprint{Packages: []string{"ripgrep"}})

	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want no profile on a box with no toolchain", profile, err)
	}
}

func TestSessionMiseIsTheOneTheToolchainStepInstalled(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	installed := filepath.Join(home, ".local", "bin", "mise")
	if err := os.MkdirAll(filepath.Dir(installed), 0o700); err != nil {
		t.Fatalf("make the install directory: %v", err)
	}
	if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write the installed mise: %v", err)
	}

	if got := SessionMise(home); got != installed {
		t.Errorf("SessionMise(%q) = %q, want %q", home, got, installed)
	}
}

func TestSessionMiseFallsBackToMiseOnPath(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	if got := SessionMise(home); got != "mise" {
		t.Errorf("SessionMise(%q) = %q, want %q", home, got, "mise")
	}
}
