package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/release"
)

// newUpdater is a darwin/arm64 smith 0.2.0 installed at path, off any box,
// whose GitHub serves the 0.3.0 release as the latest and whose replacement
// binary can continue the box step.
func newUpdater(t *testing.T, g *fakeGetter, path string) Updater {
	t.Helper()
	g.bodies[release.LatestURL] = []byte(`{"tag_name":"v0.3.0"}`)
	return Updater{
		Version:    "0.2.0",
		OS:         "darwin",
		Arch:       "arm64",
		MarkerPath: filepath.Join(t.TempDir(), "bootstrap.json"),
		Get:        g,
		Executable: func() (string, error) { return path, nil },
		Continues:  func(context.Context, string) (bool, error) { return true, nil },
	}
}

func TestRunBehindTheLatestReleaseReplacesTheBinaryAndSwitchesIntoIt(t *testing.T) {
	t.Parallel()
	g, _ := publishedRelease(t, "new smith")
	path := installedBinary(t)
	u := newUpdater(t, g, path)

	got, err := u.Run(t.Context(), "")
	if err != nil {
		t.Fatalf("Run() errored: %v", err)
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	want := Outcome{From: "0.2.0", Target: "0.3.0", Path: resolved, Next: StepSwitch}
	if got != want {
		t.Errorf("Run() = %+v, want %+v", got, want)
	}
	if content := readFile(t, path); content != "new smith" {
		t.Errorf("binary = %q, want %q", content, "new smith")
	}
}

func TestRunAlreadyOnTheTargetRunsTheBoxStepHereWithoutDownloading(t *testing.T) {
	t.Parallel()
	g, _ := publishedRelease(t, "new smith")
	u := newUpdater(t, g, installedBinary(t))
	u.Version = "0.3.0"

	got, err := u.Run(t.Context(), "v0.3.0")
	if err != nil {
		t.Fatalf("Run() errored: %v", err)
	}

	if want := (Outcome{From: "0.3.0", Target: "0.3.0", Next: StepBoxesHere}); got != want {
		t.Errorf("Run() = %+v, want %+v", got, want)
	}
	if len(g.asked) != 0 {
		t.Errorf("GitHub was asked for %v, want nothing fetched", g.asked)
	}
}

func TestRunIntoAReleaseWithNoBoxStepHandsTheBoxesOff(t *testing.T) {
	t.Parallel()
	g, _ := publishedRelease(t, "old smith")
	u := newUpdater(t, g, installedBinary(t))
	u.Continues = func(context.Context, string) (bool, error) { return false, nil }

	got, err := u.Run(t.Context(), "")
	if err != nil {
		t.Fatalf("Run() errored: %v", err)
	}

	if got.Next != StepHandOff {
		t.Errorf("Run().Next = %v, want StepHandOff", got.Next)
	}
}

func TestRunRefusesBeforeFetchingAnything(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		adapt func(t *testing.T, u *Updater)
		named string
	}{
		{"on windows", func(_ *testing.T, u *Updater) { u.OS = "windows" }, ""},
		{"on a box", func(t *testing.T, u *Updater) {
			if err := os.WriteFile(u.MarkerPath, []byte("{}"), 0o600); err != nil {
				t.Fatalf("write marker: %v", err)
			}
		}, ""},
		{"a version with no release", func(*testing.T, *Updater) {}, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, _ := publishedRelease(t, "new smith")
			u := newUpdater(t, g, installedBinary(t))
			tt.adapt(t, &u)

			_, err := u.Run(t.Context(), tt.named)

			var refused *RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("Run(%q) error = %v, want a *RefusedError", tt.named, err)
			}
			if len(g.asked) != 0 {
				t.Errorf("GitHub was asked for %v, want nothing fetched", g.asked)
			}
		})
	}
}

// fakeBoxes converges boxes by name: each answers with its canned result, or
// fails with its canned error. It records the boxes it was asked to converge.
type fakeBoxes struct {
	results map[string]onbox.Result
	errs    map[string]error
	reached []string
}

func (f *fakeBoxes) converge(_ context.Context, box, version string) (onbox.Result, error) {
	f.reached = append(f.reached, box+"@"+version)
	if err, ok := f.errs[box]; ok {
		return onbox.Result{}, err
	}
	return f.results[box], nil
}

func TestUpgradeWithAssumedYesConvergesEveryBoxInOrderWithoutAsking(t *testing.T) {
	t.Parallel()
	boxes := &fakeBoxes{results: map[string]onbox.Result{
		"alpha": {Version: "0.3.0", Previous: "0.2.0", Changed: true},
		"beta":  {Version: "0.3.0", Previous: "0.3.0"},
	}}
	b := Boxes{
		Version:  "0.3.0",
		Ask:      func([]string) (bool, error) { t.Error("asked, want no question"); return false, nil },
		Converge: boxes.converge,
	}

	got, err := b.Upgrade(t.Context(), []string{"alpha", "beta"}, true)
	if err != nil {
		t.Fatalf("Upgrade() errored: %v", err)
	}

	want := Fleet{
		{Box: "alpha", Status: StatusUpgraded, Result: boxes.results["alpha"]},
		{Box: "beta", Status: StatusCurrent, Result: boxes.results["beta"]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Upgrade() = %+v, want %+v", got, want)
	}
	if wantReached := []string{"alpha@0.3.0", "beta@0.3.0"}; !reflect.DeepEqual(boxes.reached, wantReached) {
		t.Errorf("converged %v, want %v", boxes.reached, wantReached)
	}
}

func TestUpgradeDeclinedSkipsEveryBoxAndConvergesNone(t *testing.T) {
	t.Parallel()
	boxes := &fakeBoxes{}
	var asked []string
	b := Boxes{
		Version:  "0.3.0",
		Ask:      func(names []string) (bool, error) { asked = names; return false, nil },
		Converge: boxes.converge,
	}

	got, err := b.Upgrade(t.Context(), []string{"alpha", "beta"}, false)
	if err != nil {
		t.Fatalf("Upgrade() errored: %v", err)
	}

	if want := (Fleet{{Box: "alpha", Status: StatusSkipped}, {Box: "beta", Status: StatusSkipped}}); !reflect.DeepEqual(got, want) {
		t.Errorf("Upgrade() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(asked, []string{"alpha", "beta"}) {
		t.Errorf("asked about %v, want one question naming alpha and beta", asked)
	}
	if len(boxes.reached) != 0 {
		t.Errorf("converged %v, want none", boxes.reached)
	}
}

func TestUpgradeCarriesOnPastAFailedBoxAndCountsIt(t *testing.T) {
	t.Parallel()
	unreachable := errors.New("ssh: connect to host: Connection timed out")
	boxes := &fakeBoxes{
		results: map[string]onbox.Result{"beta": {Version: "0.3.0", Previous: "0.2.0", Changed: true}},
		errs:    map[string]error{"alpha": unreachable},
	}
	b := Boxes{Version: "0.3.0", Converge: boxes.converge}

	got, err := b.Upgrade(t.Context(), []string{"alpha", "beta"}, true)
	if err != nil {
		t.Fatalf("Upgrade() errored: %v", err)
	}

	want := Fleet{
		{Box: "alpha", Status: StatusFailed, Err: unreachable},
		{Box: "beta", Status: StatusUpgraded, Result: boxes.results["beta"]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Upgrade() = %+v, want %+v", got, want)
	}
	if got.Failed() != 1 {
		t.Errorf("Failed() = %d, want 1", got.Failed())
	}
}

func TestRunWhoseProbeFailsNamesTheInstalledBinary(t *testing.T) {
	t.Parallel()
	g, _ := publishedRelease(t, "new smith")
	path := installedBinary(t)
	u := newUpdater(t, g, path)
	cause := errors.New("signal: killed")
	u.Continues = func(context.Context, string) (bool, error) { return false, cause }

	_, err := u.Run(t.Context(), "")

	var probe *ProbeError
	if !errors.As(err, &probe) {
		t.Fatalf("Run() error = %v, want a *ProbeError", err)
	}
	resolved, rerr := filepath.EvalSymlinks(path)
	if rerr != nil {
		t.Fatalf("resolve %s: %v", path, rerr)
	}
	if want := (ProbeError{Path: resolved, Target: "0.3.0", Err: cause}); *probe != want {
		t.Errorf("Run() error = %+v, want %+v", *probe, want)
	}
}
