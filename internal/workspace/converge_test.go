package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
)

// fakeBox stands in for the box the stage runs on: it records the argv of
// every command, answers the package probe from the set it holds installed,
// and can fail the install the way a box out of disk does.
type fakeBox struct {
	calls     [][]string
	installed map[string]bool
	aptOutput string
	aptErr    error
	// hasMise is whether the box already holds mise, which the install turns
	// on the way a real one does.
	hasMise bool
	// miseErr fails the mise commands the way a box with no network does.
	miseErr error
}

func (f *fakeBox) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	argv := append([]string{name}, args...)
	f.calls = append(f.calls, argv)
	line := strings.Join(argv, " ")
	switch {
	case strings.Contains(line, "dpkg-query"):
		pkg := argv[len(argv)-1]
		if !f.installed[pkg] {
			return fmt.Errorf("no packages found matching %s", pkg)
		}
		if _, err := io.WriteString(stdout, "installed\n"); err != nil {
			return fmt.Errorf("write canned status: %w", err)
		}
		return nil
	case strings.Contains(line, "mise.run"):
		f.hasMise = true
		return f.miseErr
	case strings.HasSuffix(name, "mise"):
		if !f.hasMise {
			return fmt.Errorf("mise: command not found")
		}
		if _, err := io.WriteString(stdout, "2025.8.0 macos-arm64\n"); err != nil {
			return fmt.Errorf("write canned mise output: %w", err)
		}
		return f.miseErr
	case strings.Contains(line, "apt-get"):
		if _, err := io.WriteString(stdout, f.aptOutput); err != nil {
			return fmt.Errorf("write canned apt output: %w", err)
		}
		return f.aptErr
	}
	return fmt.Errorf("unexpected command %q", line)
}

// installs returns the one apt-get invocation the run made, joined for
// readability, and fails the test when the run made none or several.
func (f *fakeBox) installs(t *testing.T) string {
	t.Helper()
	var found []string
	for _, argv := range f.calls {
		if line := strings.Join(argv, " "); strings.Contains(line, "apt-get") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("apt-get ran %d times, want 1: %v", len(found), f.calls)
	}
	return found[0]
}

// ran reports whether any command the run launched carried the given text,
// which is how a test asks what the stage did to the box.
func (f *fakeBox) ran(text string) bool {
	for _, argv := range f.calls {
		if strings.Contains(strings.Join(argv, " "), text) {
			return true
		}
	}
	return false
}

// ranApt reports whether the run reached apt-get at all.
func (f *fakeBox) ranApt() bool {
	for _, argv := range f.calls {
		if strings.Contains(strings.Join(argv, " "), "apt-get") {
			return true
		}
	}
	return false
}

// convergePackages runs the stage over one packages unit against the box and
// returns what it did and what it streamed.
func convergePackages(t *testing.T, box *fakeBox, packages ...string) (Result, string) {
	t.Helper()
	var progress bytes.Buffer
	result, err := Converge(context.Background(), Env{Command: box}, []Unit{{Step: Packages, Packages: packages}}, &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result, progress.String()
}

// TestConvergeInstallsDeclaredPackages proves the blueprint's packages reach
// apt, and that the run reports what it installed.
func TestConvergeInstallsDeclaredPackages(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{}}
	result, _ := convergePackages(t, box, "ripgrep", "jq")

	line := box.installs(t)
	if !strings.Contains(line, "install -y ripgrep jq") {
		t.Errorf("apt-get invocation = %q, want it to install ripgrep and jq", line)
	}
	if result.Failed() {
		t.Errorf("Result.Failed() = true, want false: %s", result.Report())
	}
	if summary := result.Outcomes[0].Summary; !strings.Contains(summary, "ripgrep") {
		t.Errorf("outcome summary = %q, want it to name what was installed", summary)
	}
}

// TestConvergeWaitsOutABusyAptLock proves the install carries the base layer's
// own lock-wait rather than racing the first-boot unattended-upgrades run and
// aborting.
func TestConvergeWaitsOutABusyAptLock(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{}}
	convergePackages(t, box, "ripgrep")

	line := box.installs(t)
	if wait := strings.Join(bootstrap.AptLockArgs(), " "); !strings.Contains(line, wait) {
		t.Errorf("apt-get invocation = %q, want it to carry the base layer's lock wait %q", line, wait)
	}
}

// TestConvergeReportsNothingToInstall proves an already-satisfied packages
// list is a no-op: apt is not reached at all, and the operator is told there
// was nothing to do.
func TestConvergeReportsNothingToInstall(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{"ripgrep": true, "jq": true}}
	result, progress := convergePackages(t, box, "ripgrep", "jq")

	if box.ranApt() {
		t.Errorf("apt-get ran for an already-installed package list: %v", box.calls)
	}
	if summary := result.Outcomes[0].Summary; !strings.Contains(summary, "nothing to install") {
		t.Errorf("outcome summary = %q, want it to report nothing to install", summary)
	}
	if !strings.Contains(progress, "nothing to install") {
		t.Errorf("progress = %q, want it to report nothing to install", progress)
	}
}

// TestConvergeInstallsOnlyWhatIsMissing proves a partly-satisfied list reaches
// apt with the missing package alone.
func TestConvergeInstallsOnlyWhatIsMissing(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{"ripgrep": true}}
	convergePackages(t, box, "ripgrep", "jq")

	line := box.installs(t)
	if !strings.HasSuffix(line, "install -y jq") {
		t.Errorf("apt-get invocation = %q, want it to install jq alone", line)
	}
}

// TestConvergeStreamsProgress proves the operator sees the step as it happens
// — apt's own output included — rather than a silence that ends in a summary.
func TestConvergeStreamsProgress(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{}, aptOutput: "Setting up jq (1.7.1-3build1) ...\n"}
	_, progress := convergePackages(t, box, "jq")

	if !strings.Contains(progress, "Setting up jq") {
		t.Errorf("progress = %q, want apt's own output streamed into it", progress)
	}
	if !strings.Contains(progress, string(Packages)) {
		t.Errorf("progress = %q, want the step reported as it completes", progress)
	}
}

// TestConvergeReportsAFailedStep proves a step that fails is reported with its
// cause and makes the run a failure, rather than being swallowed.
func TestConvergeReportsAFailedStep(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{}, aptErr: errors.New("exit status 100")}
	result, progress := convergePackages(t, box, "jq")

	if !result.Failed() {
		t.Errorf("Result.Failed() = false, want true: %s", result.Report())
	}
	if err := result.Outcomes[0].Err; err == nil || !strings.Contains(err.Error(), "exit status 100") {
		t.Errorf("outcome error = %v, want it to carry apt's own failure", err)
	}
	if !strings.Contains(progress, "exit status 100") {
		t.Errorf("progress = %q, want the failure reported as it happens", progress)
	}
	if !strings.Contains(result.Report(), "1 failed") {
		t.Errorf("Report() = %q, want it to count the failure", result.Report())
	}
}

// TestConvergeNothingToConverge proves an empty plan is a run that did nothing
// and failed at nothing, which is what a blueprint declaring no workspace at
// all converges to.
func TestConvergeNothingToConverge(t *testing.T) {
	box := &fakeBox{installed: map[string]bool{}}
	result, err := Converge(context.Background(), Env{Command: box}, nil, io.Discard)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	if result.Failed() {
		t.Errorf("Result.Failed() = true, want false")
	}
	if len(box.calls) != 0 {
		t.Errorf("an empty plan ran %v, want nothing", box.calls)
	}
	if !strings.Contains(result.Report(), "nothing to converge") {
		t.Errorf("Report() = %q, want it to report nothing to converge", result.Report())
	}
}
