package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// TestRemoveReclaimsTheWorktreeOfACleanStoppedSession locks in what rm costs
// when the gate lets it through: the checkout is gone and the branch it was
// working on is still in the repo, which is the whole reason the gate can be
// as narrow as it is.
func TestRemoveReclaimsTheWorktreeOfACleanStoppedSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	started := start(t, env, "smith", "spec-42")
	stop(t, env, started.Name)

	removed, err := session.Remove(context.Background(), env, []string{started.Name}, false)
	if err != nil {
		t.Fatalf("Remove() err = %v", err)
	}

	if removed[0].Branch != "spec-42" {
		t.Errorf("Remove() branch = %q, want the branch it reclaimed the worktree of", removed[0].Branch)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived the removal: %v", dir, err)
	}
	if !branchExists(t, workspace, "smith", "spec-42") {
		t.Error("branch spec-42 is gone, want the branch to survive the removal")
	}
}

// TestARemovedSessionComesBackOnStart locks in the promise the gate rests on:
// what rm deletes is a working directory, and start puts it back with the
// commits the operator left on the branch.
func TestARemovedSessionComesBackOnStart(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	commit(t, dir, "work")
	stop(t, env, started.Name)
	if _, err := session.Remove(context.Background(), env, []string{started.Name}, false); err != nil {
		t.Fatalf("Remove() err = %v", err)
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "work.txt")); err != nil {
		t.Errorf("the commit the branch carried is not in the new worktree: %v", err)
	}
}

// TestRemoveRefusesALiveSession locks in the refusal that names a process
// rather than a file: rm does not kill a driver behind the operator's back, it
// hands them the verb that ends it.
func TestRemoveRefusesALiveSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")

	_, err := session.Remove(context.Background(), env, []string{started.Name}, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want a refusal on a live session")
	}
	for _, want := range []string{"smith/smith-spec-42", "smith session stop smith-spec-42"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Remove() err = %q, want it to carry %q", err, want)
		}
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("worktree at %s did not survive the refusal: %v", dir, err)
	}
}

// TestRemoveRefusesADirtyWorktreeWithAnEnumeration locks in the shape of the
// dirty refusal: what is at risk counted the three ways git holds it, and the
// exact command that overrides it, so the operator never has to compose one.
func TestRemoveRefusesADirtyWorktreeWithAnEnumeration(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, "staged.txt"), "staged\n")
	git(t, dir, "add", "staged.txt")
	writeFile(t, filepath.Join(dir, "README.md"), "changed\n")
	writeFile(t, filepath.Join(dir, "scratch.txt"), "notes\n")
	stop(t, env, started.Name)

	_, err := session.Remove(context.Background(), env, []string{started.Name}, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want a refusal on a dirty worktree")
	}
	for _, want := range []string{"1 modified", "1 staged", "1 untracked", "smith session rm smith-spec-42 --force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Remove() err = %q, want it to carry %q", err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); err != nil {
		t.Errorf("worktree at %s did not survive the refusal: %v", dir, err)
	}
}

// TestRemoveReportsTheTwoRefusalsSeparately locks in that a running driver and
// an uncommitted file are never collapsed into one sentence: they are
// different kinds of loss and the operator acts on them differently.
func TestRemoveReportsTheTwoRefusalsSeparately(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")
	writeFile(t, filepath.Join(workspace, "smith", "worktrees", "spec-42", "README.md"), "changed\n")

	_, err := session.Remove(context.Background(), env, []string{started.Name}, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want a refusal on a live, dirty session")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 {
		t.Fatalf("Remove() err = %q, want the running session and the dirty worktree reported as two refusals", err)
	}
	if !strings.Contains(lines[0], "is running") {
		t.Errorf("first refusal = %q, want it to report the running session", lines[0])
	}
	if !strings.Contains(lines[1], "1 modified") {
		t.Errorf("second refusal = %q, want it to enumerate the dirty worktree", lines[1])
	}
}

// TestRemoveReportsUnpushedCommitsAndProceeds locks in the one thing rm counts
// without gating on: a check that fires on nearly every in-progress branch
// would train --force into muscle memory and cost the dirty gate its meaning.
func TestRemoveReportsUnpushedCommitsAndProceeds(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepoWithRemote(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	for _, message := range []string{"one", "two", "three"} {
		commit(t, dir, message)
	}
	stop(t, env, started.Name)

	removed, err := session.Remove(context.Background(), env, []string{started.Name}, false)
	if err != nil {
		t.Fatalf("Remove() err = %v, want unpushed commits reported rather than refused", err)
	}

	if removed[0].Unpushed != 3 {
		t.Errorf("Remove() unpushed = %d, want 3", removed[0].Unpushed)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived the removal: %v", dir, err)
	}
	if !branchExists(t, workspace, "smith", "spec-42") {
		t.Error("branch spec-42 is gone, want the commits kept on the branch")
	}
}

// TestForceOverridesBothRefusals locks in that one flag covers both gates: it
// ends the process and reclaims the checkout, and the branch still survives —
// force is about the worktree, never about the branch.
func TestForceOverridesBothRefusals(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, "README.md"), "changed\n")

	removed, err := session.Remove(context.Background(), env, []string{started.Name}, true)
	if err != nil {
		t.Fatalf("Remove(force) err = %v", err)
	}

	if !removed[0].Killed {
		t.Error("Remove(force) reported nothing killed, want the live tmux session ended")
	}
	if tmux.ran("kill-session") != 1 {
		t.Errorf("tmux kill-session ran %d times, want the live session killed once", tmux.ran("kill-session"))
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived Remove(force): %v", dir, err)
	}
	if !branchExists(t, workspace, "smith", "spec-42") {
		t.Error("branch spec-42 is gone, want force to reclaim the worktree and keep the branch")
	}
}

// TestRemoveTreatsAPlacedFileAsClean locks in the subtraction rm inherits from
// the work-state predicate: smith wrote those files, so they are not the
// operator's work, and the subtraction holds even where the exclude file an
// operator can edit does not.
func TestRemoveTreatsAPlacedFileAsClean(t *testing.T) {
	tests := []struct {
		name  string
		after func(t *testing.T, bare string)
	}{
		{"the repo does not ignore the placed file", func(*testing.T, string) {}},
		{"the exclude file has been emptied by hand", func(t *testing.T, bare string) {
			writeFile(t, filepath.Join(bare, "info", "exclude"), "")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeBareRepo(t, workspace, "smith", "main")
			place := stagePlacement(t, "smith", blueprint.Placement{From: "file:secrets/env", To: ".env"}, "TOKEN=staged\n")
			env := session.Env{
				Workspace: workspace,
				Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
				Git:       connection.System(),
				Tmux:      &tmuxServer{},
				Placer:    place,
			}
			started := start(t, env, "smith", "spec-42")
			stop(t, env, started.Name)
			dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
			if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
				t.Fatalf("the placement was not materialised: %v", err)
			}
			tt.after(t, filepath.Join(workspace, "smith", "repo.git"))

			if _, err := session.Remove(context.Background(), env, []string{started.Name}, false); err != nil {
				t.Fatalf("Remove() err = %v, want a placed file read as clean", err)
			}

			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("worktree at %s survived the removal: %v", dir, err)
			}
		})
	}
}

// TestRemoveCountsAFileNoPlacementDeclares locks in the other half of the
// subtraction: it removes exactly the paths the blueprint names, so anything
// else the operator left in the worktree is still their work.
func TestRemoveCountsAFileNoPlacementDeclares(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	place := stagePlacement(t, "smith", blueprint.Placement{From: "file:secrets/env", To: ".env"}, "TOKEN=staged\n")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
		Placer:    place,
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, "scratch.txt"), "notes\n")
	stop(t, env, started.Name)

	_, err := session.Remove(context.Background(), env, []string{started.Name}, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want a file no placement declares to count")
	}
	if !strings.Contains(err.Error(), "1 untracked") {
		t.Errorf("Remove() err = %q, want it to count the one file smith did not place", err)
	}
}

// TestRemoveReadsAnIgnoredFileAsClean locks in that the repo's own ignore
// rules are honoured: build output is not work, and refusing on it would fire
// on every worktree that has been built in.
func TestRemoveReadsAnIgnoredFileAsClean(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started := start(t, env, "smith", "spec-42")
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-m", "ignore")
	writeFile(t, filepath.Join(dir, "ignored.txt"), "noise\n")
	stop(t, env, started.Name)

	if _, err := session.Remove(context.Background(), env, []string{started.Name}, false); err != nil {
		t.Fatalf("Remove() err = %v, want an ignored file read as clean", err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived the removal: %v", dir, err)
	}
}

// TestRemoveRefusesANameNoSessionHolds locks in that rm addresses a session
// through the registry every other verb reads, so a typo is told plainly
// rather than guessed at a near match.
func TestRemoveRefusesANameNoSessionHolds(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	_, err := session.Remove(context.Background(), env, []string{"smith-ghost"}, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want a refusal for a name no session holds")
	}
	if !strings.Contains(err.Error(), "smith-ghost") {
		t.Errorf("Remove() err = %q, want it to name the session it could not find", err)
	}
}

// start stands a session up and fails the test if it does not.
func start(t *testing.T, env session.Env, repo, branch string) session.Session {
	t.Helper()
	started, err := session.Start(context.Background(), env, session.StartRequest{Repo: repo, Branch: branch})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	return started
}

// stop ends a session's tmux session and fails the test if it does not, which
// is how a test reaches the stopped half of the live probe.
func stop(t *testing.T, env session.Env, name string) {
	t.Helper()
	if err := session.Stop(context.Background(), env, name); err != nil {
		t.Fatalf("Stop() err = %v", err)
	}
}

// branchExists reports whether the workspace's bare repo still holds that
// branch, which is what every removal must leave behind.
func branchExists(t *testing.T, workspace, repo, branch string) bool {
	t.Helper()
	bare := filepath.Join(workspace, repo, "repo.git")
	return gitOut(t, bare, "branch", "--list", branch, "--format=%(refname:short)") == branch
}

// TestRemoveTakesSeveralNamesAtOnce locks in the batch: one call reclaims
// every worktree it was handed, which is what the names listing composes into.
func TestRemoveTakesSeveralNamesAtOnce(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	names := standDown(t, env, "one", "two", "three")

	removed, err := session.Remove(context.Background(), env, names, false)
	if err != nil {
		t.Fatalf("Remove() err = %v", err)
	}

	if len(removed) != 3 {
		t.Errorf("Remove() reported %d removals, want one per name", len(removed))
	}
	for _, branch := range []string{"one", "two", "three"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("worktree at %s survived the batch: %v", dir, err)
		}
	}
}

// TestOneOffenderRefusesTheWholeBatch locks in that removal is all-or-nothing:
// a partially applied delete is the state nobody can reason about afterwards,
// and it would leave --force covering half a batch.
func TestOneOffenderRefusesTheWholeBatch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	names := standDown(t, env, "one", "two", "three")
	writeFile(t, filepath.Join(workspace, "smith", "worktrees", "two", "scratch.txt"), "notes\n")

	_, err := session.Remove(context.Background(), env, names, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want one dirty worktree to refuse the whole batch")
	}
	for _, branch := range []string{"one", "two", "three"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("worktree at %s did not survive the refused batch: %v", dir, err)
		}
	}
}

// TestARefusedBatchEnumeratesEveryOffender locks in that the operator is told
// the whole gate in one pass: a report naming only the first offender turns a
// batch into a queue of retries.
func TestARefusedBatchEnumeratesEveryOffender(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	names := standDown(t, env, "one", "two", "three", "four")
	for _, branch := range []string{"two", "four"} {
		writeFile(t, filepath.Join(workspace, "smith", "worktrees", branch, "scratch.txt"), "notes\n")
	}

	_, err := session.Remove(context.Background(), env, names, false)

	if err == nil {
		t.Fatal("Remove() err = nil, want two dirty worktrees to refuse the batch")
	}
	for _, want := range []string{"smith-two", "smith-four"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Remove() err = %q, want it to name the offender %q", err, want)
		}
	}
}

// TestAnUnknownNameRefusesTheBatchBeforeAnythingIsRemoved locks in that the
// gate covers the names too: a typo in a batch costs nothing, rather than
// removing everything up to it.
func TestAnUnknownNameRefusesTheBatchBeforeAnythingIsRemoved(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	names := standDown(t, env, "one", "two")

	_, err := session.Remove(context.Background(), env, append(names, "smith-ghost"), false)

	if err == nil {
		t.Fatal("Remove() err = nil, want an unknown name to refuse the batch")
	}
	if !strings.Contains(err.Error(), "smith-ghost") {
		t.Errorf("Remove() err = %q, want it to name the session it could not find", err)
	}
	for _, branch := range []string{"one", "two"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("worktree at %s did not survive the refused batch: %v", dir, err)
		}
	}
}

// TestRemoveRefusesAnEmptyBatch locks in that a names listing that found
// nothing composes into a refusal rather than into a removal of everything.
func TestRemoveRefusesAnEmptyBatch(t *testing.T) {
	env := session.Env{Workspace: t.TempDir(), Git: connection.System(), Tmux: &tmuxServer{}}

	if _, err := session.Remove(context.Background(), env, nil, false); err == nil {
		t.Fatal("Remove() err = nil, want a refusal when no session was named")
	}
}

// standDown stands a stopped session up on each branch of the smith repo and
// answers with the names they are addressed by, which is the fixture every
// batch case starts from.
func standDown(t *testing.T, env session.Env, branches ...string) []string {
	t.Helper()
	names := make([]string, len(branches))
	for i, branch := range branches {
		started := start(t, env, "smith", branch)
		stop(t, env, started.Name)
		names[i] = started.Name
	}
	return names
}
