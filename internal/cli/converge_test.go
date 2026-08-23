package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/release"
)

// convergeNamed runs the named-box convergence against a config home rooted at
// dir, with the local ssh and scp answered by the given fake.
func convergeNamed(t *testing.T, dir string, ssh *upgradeSSH, box, version string) (convergence, error) {
	t.Helper()
	converge := convergeNamedBox(func() (config.Home, error) { return config.NewHome(dir), nil }, ssh)
	return converge(context.Background(), box, version)
}

// TestConvergeNamedBoxConvergesTheBoxTheInventoryNames checks the one operation
// both `machine upgrade` and an accepted skew prompt run: the box the operator
// named is resolved to its target, its binary is converged, and what happened
// comes back typed with the box named.
func TestConvergeNamedBoxConvergesTheBoxTheInventoryNames(t *testing.T) {
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64", installed: "0.1.0"}

	result, err := convergeNamed(t, dir, ssh, "dev", "0.2.0")

	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if result.Box != "dev" || result.Result.Version != "0.2.0" || result.Result.Previous != "0.1.0" {
		t.Errorf("result = %+v, want box dev moved from 0.1.0 to 0.2.0", result)
	}
	if cmd := ssh.installCommand(); !strings.Contains(cmd, release.For("0.2.0", "amd64").URL) {
		t.Errorf("install command %q does not fetch the 0.2.0 amd64 asset", cmd)
	}
	if len(ssh.targets) == 0 || ssh.targets[0] != "smith@100.92.14.7" {
		t.Errorf("reached %v, want the registered target", ssh.targets)
	}
	if report := result.Report(); !strings.Contains(report, "box dev: ") {
		t.Errorf("report = %q, does not name the box it converged", report)
	}
}

// TestConvergeNamedBoxRefusesABuildWithNoReleaseUntouched checks that the
// version policy is settled before the box is reached, so a build with nothing
// to install refuses with the box exactly as it was.
func TestConvergeNamedBoxRefusesABuildWithNoReleaseUntouched(t *testing.T) {
	dir := registeredBox(t)
	ssh := &upgradeSSH{machine: "x86_64"}

	_, err := convergeNamed(t, dir, ssh, "dev", "dev")

	if err == nil {
		t.Fatal("converge succeeded, want a build with no release asset refused")
	}
	for _, want := range []string{"dev build", "--smith-version", "smith machine upgrade dev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, does not mention %q", err, want)
		}
	}
	if len(ssh.commands) != 0 {
		t.Errorf("the box was reached with %v, want it untouched", ssh.commands)
	}
}
