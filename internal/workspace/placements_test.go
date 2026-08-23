package workspace

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/staging"
)

// box is a box with nothing installed, for the runs where the commands the
// stage drives are beside the point.
func box() *fakeBox { return &fakeBox{installed: map[string]bool{}} }

// stage writes the bytes `machine setup` would have staged for a box-scoped
// destination, under the box state directory root.
func stage(t *testing.T, root, destination, content string) {
	t.Helper()
	path := staging.BoxPlacementPathIn(root, destination)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the staged placements directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("stage the bytes of %s: %v", destination, err)
	}
}

// convergeBlueprint runs the stage over a blueprint against a box state
// directory and a home of the test's own, and returns what it did.
func convergeBlueprint(t *testing.T, b blueprint.Blueprint, root, home string) (Result, string) {
	t.Helper()
	var progress bytes.Buffer
	env := Env{Command: box(), StateRoot: root}
	result, err := Converge(context.Background(), env, Plan(b, home), &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result, progress.String()
}

// convergeAs runs the stage over a blueprint as a given account, and returns
// what it did.
func convergeAs(t *testing.T, b blueprint.Blueprint, root, home string, owner Owner) (Result, string) {
	t.Helper()
	var progress bytes.Buffer
	env := Env{Command: box(), StateRoot: root, Owner: owner}
	result, err := Converge(context.Background(), env, Plan(b, home), &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result, progress.String()
}

// declares is a blueprint declaring one box placement.
func declares(destination, mode string) blueprint.Blueprint {
	return blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "file:/home/op/secrets/npmrc", To: destination, Mode: mode},
	}}
}

// held is what the box holds at path, and fails the test when it holds
// nothing.
func held(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read what the box holds at %s: %v", path, err)
	}
	return string(data)
}

// TestConvergeWritesAPlacementFromItsStagedBytes proves a declared placement
// lands on the box carrying the permissions the operator declared for it.
func TestConvergeWritesAPlacementFromItsStagedBytes(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	b := blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "file:/home/op/secrets/npmrc", To: filepath.Join(home, ".npmrc"), Mode: "converge", Perms: "0640"},
	}}
	stage(t, root, filepath.Join(home, ".npmrc"), "//registry.npmjs.org/:_authToken=t\n")

	result, progress := convergeBlueprint(t, b, root, home)

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	path := filepath.Join(home, ".npmrc")
	if got := held(t, path); got != "//registry.npmjs.org/:_authToken=t\n" {
		t.Errorf("%s holds %q, want the staged bytes", path, got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("%s carries mode %v, want 0640", path, info.Mode().Perm())
	}
	if !strings.Contains(progress, path) {
		t.Errorf("progress = %q, want it to name the destination", progress)
	}
}

// TestConvergeResolvesATildeDestinationAgainstHome proves a destination
// written the way an operator writes it lands in the smith user's home.
func TestConvergeResolvesATildeDestinationAgainstHome(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.gitconfig", "[user]\n\tname = smith\n")

	convergeBlueprint(t, declares("~/.gitconfig", "converge"), root, home)

	if got := held(t, filepath.Join(home, ".gitconfig")); !strings.Contains(got, "name = smith") {
		t.Errorf("~/.gitconfig holds %q, want the staged bytes", got)
	}
}

// TestConvergeOverwritesAHandEditedDestination proves a converge placement is
// whole-file replacement: the blueprint's bytes win over an edit made on the
// box, which is both the point and the hazard.
func TestConvergeOverwritesAHandEditedDestination(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "declared\n")
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("edited on the box\n"), 0o600); err != nil {
		t.Fatalf("write the hand edit: %v", err)
	}

	convergeBlueprint(t, declares("~/.npmrc", "converge"), root, home)

	if got := held(t, filepath.Join(home, ".npmrc")); got != "declared\n" {
		t.Errorf("~/.npmrc holds %q, want the staged bytes back", got)
	}
}

// TestConvergeLeavesAMatchingDestinationAlone proves the pass is
// check-before-change: a destination already holding the staged bytes is not
// rewritten and its modification time does not move.
func TestConvergeLeavesAMatchingDestinationAlone(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "declared\n")
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("declared\n"), 0o600); err != nil {
		t.Fatalf("write the converged destination: %v", err)
	}
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatalf("age the destination: %v", err)
	}

	result, _ := convergeBlueprint(t, declares("~/.npmrc", "converge"), root, home)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(then) {
		t.Errorf("modification time moved to %v, want it left at %v", info.ModTime(), then)
	}
	if summary := result.Outcomes[0].Summary; !strings.Contains(summary, "unchanged") {
		t.Errorf("outcome summary = %q, want it to report the destination unchanged", summary)
	}
}

// TestConvergeWritesAOncePlacementWhenAbsent proves a once placement is seeded
// on a box that does not hold it.
func TestConvergeWritesAOncePlacementWhenAbsent(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "seeded\n")

	convergeBlueprint(t, declares("~/.npmrc", "once"), root, home)

	if got := held(t, filepath.Join(home, ".npmrc")); got != "seeded\n" {
		t.Errorf("~/.npmrc holds %q, want the staged bytes", got)
	}
}

// TestConvergeKeepsAnEditedOncePlacement proves the box owns a once placement
// after it is seeded: an edit made on the box survives the next run.
func TestConvergeKeepsAnEditedOncePlacement(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "seeded\n")
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("edited on the box\n"), 0o600); err != nil {
		t.Fatalf("write the hand edit: %v", err)
	}

	convergeBlueprint(t, declares("~/.npmrc", "once"), root, home)

	if got := held(t, path); got != "edited on the box\n" {
		t.Errorf("~/.npmrc holds %q, want the edit kept", got)
	}
}

// TestConvergeReseedsADeletedOncePlacement proves identity is path existence:
// deleting the destination is how an operator asks for the staged bytes back.
func TestConvergeReseedsADeletedOncePlacement(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "seeded\n")
	b := declares("~/.npmrc", "once")
	convergeBlueprint(t, b, root, home)
	path := filepath.Join(home, ".npmrc")
	if err := os.Remove(path); err != nil {
		t.Fatalf("delete the destination: %v", err)
	}

	convergeBlueprint(t, b, root, home)

	if got := held(t, path); got != "seeded\n" {
		t.Errorf("~/.npmrc holds %q, want it re-seeded", got)
	}
}

// TestConvergeRefusesAPlacementWithNoStagedBytes proves a placement whose
// bytes are not staged beside the document is refused by name, pointing at the
// one command that stages them — and that no source reference is resolved on
// the box to make up for it, for any scheme.
func TestConvergeRefusesAPlacementWithNoStagedBytes(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	source := filepath.Join(t.TempDir(), "npmrc")
	if err := os.WriteFile(source, []byte("laptop bytes\n"), 0o600); err != nil {
		t.Fatalf("write the operator-side source: %v", err)
	}
	b := blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "file:" + source, To: "~/.npmrc", Mode: "converge"},
	}}

	result, progress := convergeBlueprint(t, b, root, home)

	if !result.Failed() {
		t.Fatalf("Result.Failed() = false, want true: %s", result.Report())
	}
	err := result.Outcomes[0].Err
	if err == nil || !strings.Contains(err.Error(), "~/.npmrc") || !strings.Contains(err.Error(), "machine setup") {
		t.Errorf("refusal = %v, want it to name the destination and machine setup", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".npmrc")); !os.IsNotExist(statErr) {
		t.Errorf("the box holds ~/.npmrc, want a run that resolved no source reference of its own")
	}
	if !strings.Contains(progress, "machine setup") {
		t.Errorf("progress = %q, want the refusal reported as it happens", progress)
	}
}

// TestConvergeWritesEveryPlacementItCan proves failure is per-unit: a
// placement with no staged bytes does not stop the ones declared after it, and
// every failure is reported rather than the first alone.
func TestConvergeWritesEveryPlacementItCan(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.gitconfig", "[user]\n")
	b := blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "file:/home/op/npmrc", To: "~/.npmrc", Mode: "converge"},
		{From: "file:/home/op/gitconfig", To: "~/.gitconfig", Mode: "converge"},
	}}

	result, _ := convergeBlueprint(t, b, root, home)

	if _, err := os.Stat(filepath.Join(home, ".gitconfig")); err != nil {
		t.Errorf("~/.gitconfig was not written after an earlier placement failed: %v", err)
	}
	if !result.Failed() || len(result.Outcomes) != 2 {
		t.Fatalf("Result = %+v, want both placements reported with one failure", result.Outcomes)
	}
}

// TestConvergeWritesPlacementsBeforeTheCommandsThatNeedThem proves the
// ordering rule as behavior: the box's git identity is on disk before the
// stage runs a command, so an authenticated clone cannot fail as a credential
// bug that misdirects the operator entirely.
func TestConvergeWritesPlacementsBeforeTheCommandsThatNeedThem(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.gitconfig", "[user]\n")
	path := filepath.Join(home, ".gitconfig")
	watcher := &watchingBox{fakeBox: fakeBox{installed: map[string]bool{}}, path: path}
	b := blueprint.Blueprint{
		Packages:   []string{"ripgrep"},
		Placements: []blueprint.Placement{{From: "file:/home/op/gitconfig", To: "~/.gitconfig", Mode: "converge"}},
	}

	var progress bytes.Buffer
	if _, err := Converge(context.Background(), Env{Command: watcher, StateRoot: root}, Plan(b, home), &progress); err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}

	if !watcher.placed {
		t.Errorf("the stage ran a command before %s was on the box", path)
	}
}

// watchingBox is a box that notices whether a placement is already on disk by
// the time the stage runs its first command.
type watchingBox struct {
	fakeBox
	path   string
	placed bool
	seen   bool
}

func (w *watchingBox) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if !w.seen {
		w.seen = true
		_, err := os.Stat(w.path)
		w.placed = err == nil
	}
	return w.fakeBox.Run(ctx, name, args, stdin, stdout, stderr)
}

// TestConvergeRefusesAOncePlacementWithNoStagedBytes proves an existing
// destination does not excuse smith from checking the staging pass: a once
// placement is validated on every run, so an incomplete or corrupt staging is
// reported rather than hidden by the file the box already owns — and the file
// the box owns is still left alone.
func TestConvergeRefusesAOncePlacementWithNoStagedBytes(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("owned by the box\n"), 0o600); err != nil {
		t.Fatalf("write what the box owns: %v", err)
	}

	result, progress := convergeBlueprint(t, declares("~/.npmrc", "once"), root, home)

	if !result.Failed() {
		t.Fatalf("Result.Failed() = false, want true: %s", result.Report())
	}
	err := result.Outcomes[0].Err
	if err == nil || !strings.Contains(err.Error(), "~/.npmrc") || !strings.Contains(err.Error(), "machine setup") {
		t.Errorf("refusal = %v, want it to name the destination and machine setup", err)
	}
	if got := held(t, path); got != "owned by the box\n" {
		t.Errorf("~/.npmrc holds %q, want the file the box owns left alone", got)
	}
	if !strings.Contains(progress, "machine setup") {
		t.Errorf("progress = %q, want the refusal reported as it happens", progress)
	}
}

// TestConvergeCorrectsThePermissionsOfAMatchingDestination proves metadata
// converges independently of content: a destination already holding the staged
// bytes under a wider mode than the blueprint declares is narrowed to it,
// without its bytes being rewritten.
func TestConvergeCorrectsThePermissionsOfAMatchingDestination(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "declared\n")
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("declared\n"), 0o644); err != nil {
		t.Fatalf("write the converged destination: %v", err)
	}
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatalf("age the destination: %v", err)
	}

	result, progress := convergeBlueprint(t, declares("~/.npmrc", "converge"), root, home)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s carries mode %v, want 0600", path, info.Mode().Perm())
	}
	if !info.ModTime().Equal(then) {
		t.Errorf("modification time moved to %v, want the bytes left unwritten at %v", info.ModTime(), then)
	}
	if summary := result.Outcomes[0].Summary; !strings.Contains(summary, "permissions") {
		t.Errorf("outcome summary = %q, want it to report the permissions corrected", summary)
	}
	if !strings.Contains(progress, path) {
		t.Errorf("progress = %q, want it to name the destination", progress)
	}
}

// strangerOwner is an account every destination on the box belongs to someone
// else under, so a run reconciling ownership has something to correct without
// the root privileges a real chown of another account's file needs.
type strangerOwner struct{ claimed []string }

func (o *strangerOwner) Claim(path string) (bool, error) {
	o.claimed = append(o.claimed, path)
	return true, nil
}

// settledOwner is an account every destination already belongs to.
type settledOwner struct{}

func (settledOwner) Claim(string) (bool, error) { return false, nil }

// TestConvergeClaimsAMatchingDestinationForTheSmithUser proves ownership
// converges alongside permissions: a destination holding the staged bytes but
// belonging to another account is claimed for the smith user, whose credential
// it is, without its bytes being rewritten.
func TestConvergeClaimsAMatchingDestinationForTheSmithUser(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "declared\n")
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("declared\n"), 0o600); err != nil {
		t.Fatalf("write the converged destination: %v", err)
	}
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatalf("age the destination: %v", err)
	}
	owner := &strangerOwner{}

	result, _ := convergeAs(t, declares("~/.npmrc", "converge"), root, home, owner)

	if len(owner.claimed) != 1 || owner.claimed[0] != path {
		t.Errorf("claimed = %v, want the destination claimed once", owner.claimed)
	}
	if summary := result.Outcomes[0].Summary; !strings.Contains(summary, "ownership") {
		t.Errorf("outcome summary = %q, want it to report the ownership corrected", summary)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(then) {
		t.Errorf("modification time moved to %v, want the bytes left unwritten at %v", info.ModTime(), then)
	}
}

// TestConvergeLeavesAFullyConvergedDestinationAlone proves the no-op case
// survives metadata reconciliation: content, permissions and ownership all
// matching is still reported as unchanged.
func TestConvergeLeavesAFullyConvergedDestinationAlone(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "declared\n")
	path := filepath.Join(home, ".npmrc")
	if err := os.WriteFile(path, []byte("declared\n"), 0o600); err != nil {
		t.Fatalf("write the converged destination: %v", err)
	}

	result, _ := convergeAs(t, declares("~/.npmrc", "converge"), root, home, settledOwner{})

	if summary := result.Outcomes[0].Summary; summary != "unchanged "+path {
		t.Errorf("outcome summary = %q, want the destination reported unchanged", summary)
	}
}
