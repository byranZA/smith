package checkout

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

type execGit struct{}

func (execGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	return c.Run()
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixture builds an acme/api checkout holding .env and a config home declaring it.
func fixture(t *testing.T, ignore string) (checkout string, home config.Home) {
	t.Helper()
	checkout = t.TempDir()
	gitIn(t, checkout, "init", "-q")
	gitIn(t, checkout, "remote", "add", "origin", "https://github.com/acme/api")
	write(t, filepath.Join(checkout, ".gitignore"), ignore)
	write(t, filepath.Join(checkout, ".env"), "SECRET=1\n")
	cfg := t.TempDir()
	write(t, filepath.Join(cfg, "blueprints", "acme.yaml"), `repos:
  - url: https://github.com/acme/api
    placements:
      - from: file:~/api.env
        to: .env
`)
	return checkout, config.NewHome(cfg)
}

func resolve(t *testing.T, dir string, home config.Home) (string, error) {
	t.Helper()
	p, err := Placements(t.Context(), dir, home, execGit{}, io.Discard)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "nil", nil
	}
	wt := t.TempDir()
	if err := p.Placer.Place(p.Repo, wt); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestPlacementsCopiesDeclaredSourceFromTheCheckoutTopLevel(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")
	sub := filepath.Join(checkout, "pkg")
	write(t, filepath.Join(sub, "x"), "")

	got, err := resolve(t, sub, home)

	if err != nil || got != "SECRET=1\n" {
		t.Errorf("got %q, %v; want the checkout's .env bytes", got, err)
	}
}

func TestPlacementsNamesTheDestinationsAndTheRepo(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")

	p, err := Placements(t.Context(), checkout, home, execGit{}, io.Discard)

	if err != nil || p == nil || p.Repo != filepath.Base(checkout) || !reflect.DeepEqual(p.To, []string{".env"}) {
		t.Errorf("got %+v, %v; want repo %q placing .env", p, err, filepath.Base(checkout))
	}
}

func TestPlacementsOfAnUndeclaredOriginIsNil(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")
	gitIn(t, checkout, "remote", "set-url", "origin", "https://github.com/acme/other")

	got, err := resolve(t, checkout, home)

	if err != nil || got != "nil" {
		t.Errorf("got %q, %v; want nil placements", got, err)
	}
}

func TestPlacementsOutsideAGitCheckoutIsNil(t *testing.T) {
	t.Parallel()
	_, home := fixture(t, ".env\n")

	got, err := resolve(t, t.TempDir(), home)

	if err != nil || got != "nil" {
		t.Errorf("got %q, %v; want nil placements", got, err)
	}
}

func TestPlacementsRefusesASourceMissingFromTheCheckout(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")
	if err := os.Remove(filepath.Join(checkout, ".env")); err != nil {
		t.Fatal(err)
	}

	_, err := resolve(t, checkout, home)

	if err == nil || !strings.Contains(err.Error(), "missing from the checkout") || !strings.Contains(err.Error(), "acme.yaml") {
		t.Errorf("got %v; want a refusal naming the missing source and blueprint", err)
	}
}

func TestPlacementsRefusesATrackedSource(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, "other\n")
	gitIn(t, checkout, "add", ".env")

	_, err := resolve(t, checkout, home)

	if err == nil || !strings.Contains(err.Error(), "tracked file") {
		t.Errorf("got %v; want a tracked-file refusal", err)
	}
}

func TestPlacementsRefusesASourceTheCheckoutDoesNotIgnore(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, "other\n")

	_, err := resolve(t, checkout, home)

	if err == nil || !strings.Contains(err.Error(), ".gitignore") {
		t.Errorf("got %v; want a not-gitignored refusal", err)
	}
}

func TestPlacementsOfACheckoutWithoutOriginIsNil(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")
	gitIn(t, checkout, "remote", "remove", "origin")

	got, err := resolve(t, checkout, home)

	if err != nil || got != "nil" {
		t.Errorf("got %q, %v; want nil placements", got, err)
	}
}

// failingGit fails every git run whose arguments contain failOn, writing a
// diagnostic to stderr, and otherwise runs real git.
type failingGit struct {
	failOn string
	err    error
}

func (f failingGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	for _, a := range args {
		if a == f.failOn {
			_, _ = io.WriteString(stderr, "fatal: boom diagnostic")
			return f.err
		}
	}
	return execGit{}.Run(ctx, name, args, stdin, stdout, stderr)
}

func TestPlacementsReturnsAFailedProbeInsteadOfDisablingPlacements(t *testing.T) {
	t.Parallel()
	for _, probe := range []string{"rev-parse", "get-url"} {
		t.Run(probe, func(t *testing.T) {
			t.Parallel()
			checkout, home := fixture(t, ".env\n")
			cause := errors.New("exec failed")

			p, err := Placements(t.Context(), checkout, home, failingGit{failOn: probe, err: cause}, io.Discard)

			if p != nil || !errors.Is(err, cause) || !strings.Contains(err.Error(), "boom diagnostic") {
				t.Errorf("got %+v, %v; want the probe failure with its diagnostic", p, err)
			}
		})
	}
}

func TestPlacementsKeepsCancellationIdentifiable(t *testing.T) {
	t.Parallel()
	checkout, home := fixture(t, ".env\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Placements(ctx, checkout, home, execGit{}, io.Discard)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v; want context.Canceled", err)
	}
}
