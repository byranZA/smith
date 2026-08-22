package staging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/secret"
)

// fakeBox stands in for a box reached over ssh. It records every remote command
// and the stdin each one was handed, answers the digest probe with a canned
// digest, and lets a write be failed.
type fakeBox struct {
	digest   string
	digests  map[string]string
	staged   []string
	writeErr error

	commands []string
	inputs   []string
}

func (f *fakeBox) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	f.commands = append(f.commands, cmd)
	switch {
	case strings.Contains(cmd, "sha256sum"):
		if _, err := io.WriteString(stdout, f.digestFor(cmd)); err != nil {
			return err
		}
	case strings.Contains(cmd, "find"):
		for _, path := range f.staged {
			if _, err := io.WriteString(stdout, path+"\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

// digestFor answers the digest probe with whatever the box is set up to hold at
// the path the probe names, falling back to the box-wide digest.
func (f *fakeBox) digestFor(cmd string) string {
	for path, d := range f.digests {
		if strings.Contains(cmd, path) {
			return d
		}
	}
	return f.digest
}

func (f *fakeBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	f.commands = append(f.commands, cmd)
	f.inputs = append(f.inputs, string(data))
	return f.writeErr
}

func digestOf(document string) string {
	sum := sha256.Sum256([]byte(document))
	return hex.EncodeToString(sum[:]) + "  " + DocumentPath + "\n"
}

func TestConvergeStagesTheDocumentByteForByte(t *testing.T) {
	document := "access: tailscale\nprovider:\n  create: [hcloud]\n"
	box := &fakeBox{}

	result, err := Converge(context.Background(), box, Plan([]byte(document), blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.inputs) != 1 || box.inputs[0] != document {
		t.Errorf("stdin delivered = %q, want exactly %q", box.inputs, document)
	}
	if !strings.Contains(result.Report(), "staged") {
		t.Errorf("Report() = %q, want it to report the document as staged", result.Report())
	}
}

func TestConvergePutsTheDocumentInPlaceRootOwnedAt0644(t *testing.T) {
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	joined := strings.Join(box.commands, "\n")
	for _, want := range []string{"chmod 0644", "chown root:root", "mv -f", DocumentPath} {
		if !strings.Contains(joined, want) {
			t.Errorf("commands missing %q:\n%s", want, joined)
		}
	}
}

func TestConvergeLeavesAnUnchangedDocumentAlone(t *testing.T) {
	document := "access: public\n"
	box := &fakeBox{digest: digestOf(document)}

	result, err := Converge(context.Background(), box, Plan([]byte(document), blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.inputs) != 0 {
		t.Errorf("wrote %q to an unchanged box, want no write at all", box.inputs)
	}
	joined := strings.Join(box.commands, "\n")
	if strings.Contains(joined, "mv ") || strings.Contains(joined, "tee ") {
		t.Errorf("an unchanged document was rewritten:\n%s", joined)
	}
	if !strings.Contains(result.Report(), "unchanged") {
		t.Errorf("Report() = %q, want it to report the document as unchanged", result.Report())
	}
}

func TestConvergeReplacesAnEditedDocumentWholesale(t *testing.T) {
	staged := "access: public\nrepos:\n  - name: api\n"
	edited := "access: public\n"
	box := &fakeBox{digest: digestOf(staged)}

	if _, err := Converge(context.Background(), box, Plan([]byte(edited), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.inputs) != 1 || box.inputs[0] != edited {
		t.Fatalf("stdin delivered = %q, want exactly %q", box.inputs, edited)
	}
	joined := strings.Join(box.commands, "\n")
	if strings.Contains(joined, ">>") {
		t.Errorf("the document was appended to rather than replaced:\n%s", joined)
	}
	if strings.Contains(joined, "api") {
		t.Errorf("the removed repo survived the run:\n%s", joined)
	}
}

func TestConvergeCreatesNoOperatorConfigHomeOnTheBox(t *testing.T) {
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, cmd := range box.commands {
		if strings.Contains(cmd, ".smith") {
			t.Errorf("command touches an operator config home: %q", cmd)
		}
	}
}

func TestConvergeDeliversTheDocumentOverStdinNotArgv(t *testing.T) {
	document := "env:\n  NPM_TOKEN: env:NPM_TOKEN\n"
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte(document), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, cmd := range box.commands {
		if strings.Contains(cmd, "NPM_TOKEN") {
			t.Errorf("document content reached a command line: %q", cmd)
		}
	}
}

func TestConvergeNamesTheWriteThatFailed(t *testing.T) {
	box := &fakeBox{writeErr: errors.New("permission denied")}
	_, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{}))
	if err == nil {
		t.Fatal("Converge() error = nil, want the failed write reported")
	}
	if !strings.Contains(err.Error(), DocumentPath) {
		t.Errorf("Converge() error = %v, want it to name %s", err, DocumentPath)
	}
}

func TestConvergeStagesABoxPlacementsBytesUnderItsKey(t *testing.T) {
	t.Setenv("NPM_TOKEN", "s3cr3t-token")
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "~/.npmrc", Perms: "0640"}}}
	tree, err := Resolve(Plan([]byte("access: public\n"), b), secret.Resolve)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	box := &fakeBox{}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	key := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	joined := strings.Join(box.commands, "\n")
	for _, want := range []string{key, "chmod 0640", "chown smith:smith"} {
		if !strings.Contains(joined, want) {
			t.Errorf("commands missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(result.Report(), key) {
		t.Errorf("Report() = %q, want it to name the staged placement %s", result.Report(), key)
	}
}

func TestConvergeDeliversPlacementBytesOverStdinNotArgv(t *testing.T) {
	const credential = "s3cr3t-token"
	t.Setenv("NPM_TOKEN", credential)
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "/home/smith/.npmrc"}}}
	tree, err := Resolve(Plan([]byte("access: public\n"), b), secret.Resolve)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	box := &fakeBox{}

	if _, err := Converge(context.Background(), box, tree); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, cmd := range box.commands {
		if strings.Contains(cmd, credential) {
			t.Errorf("the credential reached a command line: %q", cmd)
		}
	}
	var delivered bool
	for _, in := range box.inputs {
		if in == credential {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("stdin carried %q, want the credential among it", box.inputs)
	}
}

func TestConvergeMakesThePlacementsDirectorySmithOwnedAt0700(t *testing.T) {
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	joined := strings.Join(box.commands, "\n")
	want := "install -d -m 0700 -o smith -g smith " + connection.ShellArg(PlacementsDir)
	if !strings.Contains(joined, want) {
		t.Errorf("commands do not make %s smith-owned at 0700:\n%s", PlacementsDir, joined)
	}
}

func TestConvergeStagesOnlyTheDocumentWhenNoPlacementsAreDeclared(t *testing.T) {
	box := &fakeBox{}
	result, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.inputs) != 1 {
		t.Errorf("stdin delivered %q, want the document alone", box.inputs)
	}
	if len(result.Entries) != 1 || result.Entries[0].Path != DocumentPath {
		t.Errorf("Report() covered %v, want the document alone", result.Entries)
	}
}

// resolvedTree plans a blueprint and attaches canned bytes to every placement,
// standing in for sources that resolve only on the operator's machine.
func resolvedTree(t *testing.T, b blueprint.Blueprint) Tree {
	t.Helper()
	tree, err := Resolve(Plan([]byte("access: public\n"), b), func(ref string) (string, error) {
		return "bytes of " + ref, nil
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return tree
}

func pruneOf(commands []string, path string) bool {
	for _, cmd := range commands {
		if strings.Contains(cmd, "rm -rf") && strings.Contains(cmd, connection.ShellArg(path)) {
			return true
		}
	}
	return false
}

func TestConvergePrunesAPlacementRemovedFromTheBlueprint(t *testing.T) {
	kept := BoxPlacementPathIn(Root, "/home/smith/.gitconfig")
	removed := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	tree := resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:GIT_CONFIG", To: "/home/smith/.gitconfig"},
	}})
	box := &fakeBox{staged: []string{boxDirIn(Root), kept, removed}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !pruneOf(box.commands, removed) {
		t.Errorf("the removed placement was not deleted:\n%s", strings.Join(box.commands, "\n"))
	}
	if pruneOf(box.commands, kept) {
		t.Errorf("a declared placement was deleted:\n%s", strings.Join(box.commands, "\n"))
	}
	if !strings.Contains(result.Report(), "pruned") || !strings.Contains(result.Report(), removed) {
		t.Errorf("Report() = %q, want it to report %s as pruned", result.Report(), removed)
	}
}

func TestConvergePrunesTheStagedDirectoryOfARemovedRepo(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "web", Placements: []blueprint.Placement{{From: "env:WEB_ENV", To: ".env"}}},
	}})
	gone := RepoPlacementPathIn(Root, "api", ".env")
	repoDir := filepath.Join(repoDirIn(Root), "api")
	box := &fakeBox{staged: []string{
		repoDirIn(Root),
		filepath.Join(repoDirIn(Root), "web"),
		RepoPlacementPathIn(Root, "web", ".env"),
		repoDir,
		gone,
	}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !pruneOf(box.commands, repoDir) {
		t.Errorf("the removed repo's staged directory survived:\n%s", strings.Join(box.commands, "\n"))
	}
	if pruneOf(box.commands, gone) {
		t.Errorf("a file inside a pruned directory was deleted separately:\n%s", strings.Join(box.commands, "\n"))
	}
	if strings.Count(result.Report(), "pruned") != 1 {
		t.Errorf("Report() = %q, want the pruned repo reported once", result.Report())
	}
}

func TestConvergePrunesAStrayFileLeftInTheStagedTree(t *testing.T) {
	stray := filepath.Join(PlacementsDir, "left-by-a-human")
	box := &fakeBox{staged: []string{stray}}

	result, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !pruneOf(box.commands, stray) {
		t.Errorf("the stray file survived:\n%s", strings.Join(box.commands, "\n"))
	}
	if !strings.Contains(result.Report(), stray) {
		t.Errorf("Report() = %q, want it to report %s as pruned", result.Report(), stray)
	}
}

func TestConvergeReportsAChangedSourceAsUpdated(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "api", Placements: []blueprint.Placement{{From: "env:API_ENV", To: ".env"}}},
	}})
	key := RepoPlacementPathIn(Root, "api", ".env")
	box := &fakeBox{
		digests: map[string]string{key: sum([]byte("the old bytes")) + "  " + key + "\n"},
		staged:  []string{key},
	}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !strings.Contains(result.Report(), "updated") {
		t.Errorf("Report() = %q, want the changed placement reported as updated", result.Report())
	}
	var delivered bool
	for _, in := range box.inputs {
		if in == "bytes of env:API_ENV" {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("stdin carried %q, want the new bytes among it", box.inputs)
	}
}

func TestConvergeLeavesAnUnchangedPlacementAlone(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:NPM_TOKEN", To: "~/.npmrc"},
	}})
	key := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	box := &fakeBox{
		digests: map[string]string{key: sum([]byte("bytes of env:NPM_TOKEN")) + "  " + key + "\n"},
		staged:  []string{boxDirIn(Root), key},
	}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, in := range box.inputs {
		if in == "bytes of env:NPM_TOKEN" {
			t.Errorf("an unchanged placement was rewritten with %q", in)
		}
	}
	if pruneOf(box.commands, key) {
		t.Errorf("a declared placement was pruned:\n%s", strings.Join(box.commands, "\n"))
	}
	for _, e := range result.Entries {
		if e.Path == key && e.Change != Unchanged {
			t.Errorf("%s reported as %v, want unchanged", key, e.Change)
		}
	}
}

func TestConvergeTouchesNothingOutsideTheBoxConfigDirectory(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{
		Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "/home/smith/.npmrc"}},
		Repos: []blueprint.Repo{
			{Name: "api", Placements: []blueprint.Placement{{From: "env:API_ENV", To: ".env"}}},
		},
	})
	box := &fakeBox{staged: []string{filepath.Join(PlacementsDir, "stray")}}

	if _, err := Converge(context.Background(), box, tree); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, cmd := range box.commands {
		for _, field := range strings.Fields(cmd) {
			path := strings.Trim(field, "'")
			if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, Root) {
				t.Errorf("command reaches outside %s: %q", Root, cmd)
			}
		}
	}
}
