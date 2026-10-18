package workspace

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/staging"
)

// realGit runs git as a user whose home is home, with no system, XDG or
// environment config of the test machine's leaking in.
type realGit struct{ home string }

func (g realGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Env = []string{"HOME=" + g.home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}
	return cmd.Run() //nolint:wrapcheck // a test runner hands git's own error back as the box would
}

// xdgGitConfig is a placed ~/.config/git/config carrying a credential helper.
const xdgGitConfig = "[credential]\n\thelper = store\n"

// withXDGGitConfig is a home holding ~/.config/git/config and no ~/.gitconfig.
func withXDGGitConfig(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	path := filepath.Join(home, ".config", "git", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the XDG git config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(xdgGitConfig), 0o600); err != nil {
		t.Fatalf("write the XDG git config: %v", err)
	}
	return home
}

// declaresIdentity is the resolution declaring the ada identity.
func declaresIdentity() staging.Resolution {
	r := resolved
	r.Git = ada
	return r
}

func TestConvergeIdentityWritesToGitconfigBesideAnXDGConfig(t *testing.T) {
	t.Parallel()
	home := withXDGGitConfig(t)
	env := Env{Command: realGit{home}, StateRoot: t.TempDir()}

	result, err := Converge(t.Context(), env, Plan(blueprint.Blueprint{}, declaresIdentity(), home), io.Discard)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	want := "[user]\n\tname = Ada\n\temail = ada@example.com\n"
	if got := held(t, filepath.Join(home, ".gitconfig")); got != want {
		t.Errorf("~/.gitconfig holds %q, want %q", got, want)
	}
	if got := held(t, filepath.Join(home, ".config", "git", "config")); got != xdgGitConfig {
		t.Errorf("~/.config/git/config holds %q, want it left as %q", got, xdgGitConfig)
	}
}

func TestConvergeIdentityBesideAnXDGPlacementSettlesOnTheSecondRun(t *testing.T) {
	t.Parallel()
	home := withXDGGitConfig(t)
	root := t.TempDir()
	stage(t, root, "~/.config/git/config", xdgGitConfig)
	b := declares("~/.config/git/config", "converge")
	env := Env{Command: realGit{home}, StateRoot: root}
	plan := Plan(b, declaresIdentity(), home)

	if _, err := Converge(t.Context(), env, plan, io.Discard); err != nil {
		t.Fatalf("first Converge() error = %v, want nil", err)
	}
	var progress bytes.Buffer
	result, err := Converge(t.Context(), env, plan, &progress)
	if err != nil {
		t.Fatalf("second Converge() error = %v, want nil", err)
	}

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	var got []string
	for _, o := range result.Outcomes {
		if o.Step == Placements || o.Step == Identity {
			got = append(got, string(o.Step)+": "+o.Summary)
		}
	}
	want := []string{
		"placements: unchanged " + filepath.Join(home, ".config", "git", "config"),
		"identity: user.name, user.email already set",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("second run outcomes =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
