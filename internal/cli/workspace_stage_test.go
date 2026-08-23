package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
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

// TestConvergeWorkspaceReportsAStageTheBoxRefused proves a stage the box
// refused is reported as a failed stage, which the pipeline turns into a
// provisioned box and the partial exit, rather than a setup that claims to have
// converged one.
func TestConvergeWorkspaceReportsAStageTheBoxRefused(t *testing.T) {
	dir := t.TempDir()
	writeBlueprint(t, dir, "acme", "packages:\n  - ripgrep\n")
	ssh := &fakeSSHRelay{err: relayExit(1)}
	var out, errBuf bytes.Buffer

	err := convergeWorkspace(context.Background(), ssh, "smith@10.0.0.4", "0.2.0", stagedOrFatal(t, dir, "acme"), &out, &errBuf)

	if err == nil {
		t.Fatal("convergeWorkspace() err = nil, want the box's refusal reported")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Errorf("convergeWorkspace() err = %v, want it to name the stage that failed", err)
	}
}
