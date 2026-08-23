package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
)

// TestConvergeWorkspaceRelaysTheStageToTheSmithUser proves the last stage of
// the pipeline runs on the box rather than on the operator's machine: it
// travels over ssh, as the smith user, to the smith installed there.
func TestConvergeWorkspaceRelaysTheStageToTheBox(t *testing.T) {
	ssh := &fakeSSHRelay{stdout: "workspace: 1 step converged\n"}
	var out, errBuf bytes.Buffer

	if err := convergeWorkspace(context.Background(), ssh, "smith@10.0.0.4", "0.2.0", stagedOrFatal(t, t.TempDir(), ""), &out, &errBuf); err != nil {
		t.Fatalf("convergeWorkspace() err = %v, want nil", err)
	}
	if len(ssh.calls) != 0 {
		t.Fatalf("a setup naming no blueprint relayed %v, want nothing", ssh.calls)
	}

	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "packages:\n  - ripgrep\n")
	if err := convergeWorkspace(context.Background(), ssh, "smith@10.0.0.4", "0.2.0", stagedOrFatal(t, dir, "acme"), &out, &errBuf); err != nil {
		t.Fatalf("convergeWorkspace() err = %v, want nil", err)
	}
	line := ssh.line(t)
	for _, want := range []string{"smith@10.0.0.4", "/usr/local/bin/smith", "--relayed-from '0.2.0'", "'workspace' 'converge'"} {
		if !strings.Contains(line, want) {
			t.Errorf("ssh argv = %q, want it to contain %q", line, want)
		}
	}
	if !strings.Contains(out.String(), "1 step converged") {
		t.Errorf("stdout = %q, want the box's own report", out.String())
	}
}

// TestConvergeWorkspaceReportsAFailedStageAsPartial proves a stage the box
// refused leaves the operator with a provisioned box and the partial exit,
// rather than a setup that claims to have converged one.
func TestConvergeWorkspaceReportsAFailedStageAsPartial(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "packages:\n  - ripgrep\n")
	ssh := &fakeSSHRelay{err: relayExit(1)}
	var out, errBuf bytes.Buffer

	err := convergeWorkspace(context.Background(), ssh, "smith@10.0.0.4", "0.2.0", stagedOrFatal(t, dir, "acme"), &out, &errBuf)

	var exit *exitError
	if !errors.As(err, &exit) || exit.code != bootstrap.OutcomePartial.ExitCode() {
		t.Fatalf("convergeWorkspace() err = %v, want the partial exit", err)
	}
	if !strings.Contains(errBuf.String(), "workspace") {
		t.Errorf("stderr = %q, want it to name the stage that failed", errBuf.String())
	}
}
