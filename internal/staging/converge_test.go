package staging

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/secret"
)

// fakeBox stands in for a box reached over ssh — the system boundary Converge
// works through, and the only thing faked here.
//
// It is driven by what the box holds rather than by the text of the commands it
// is handed: files answers the digest probe and tree answers the listing, both
// keyed by path. A command is recognized by rebuilding it from the path it
// would name, so the remote recipe can change without a test changing with it.
type fakeBox struct {
	// files is what the box already holds, keyed by path. The digest probe for
	// any path not here answers as an absent file does.
	files map[string]string
	// tree is what the box lists under the placements directory, which is what
	// a prune is computed against.
	tree []string
	// writeErr rejects every write, standing in for a box that refuses one.
	writeErr error

	commands []string
	writes   []delivery
}

// delivery is one write the box was handed: the command, and the bytes that
// came over stdin with it.
type delivery struct {
	cmd   string
	bytes string
}

func (f *fakeBox) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	f.commands = append(f.commands, cmd)
	if cmd == listCommand() {
		return f.list(stdout)
	}
	for path, content := range f.files {
		if cmd == digestCommand(path) {
			_, err := io.WriteString(stdout, sum([]byte(content))+"  "+path+"\n")
			return err
		}
	}
	return nil
}

func (f *fakeBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	f.commands = append(f.commands, cmd)
	f.writes = append(f.writes, delivery{cmd: cmd, bytes: string(data)})
	return f.writeErr
}

// list answers the listing of the placements directory, as find reports it.
func (f *fakeBox) list(stdout io.Writer) error {
	paths := append([]string(nil), f.tree...)
	sort.Strings(paths)
	for _, path := range paths {
		if _, err := io.WriteString(stdout, path+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// wrote reports whether the box was handed exactly this file: the write
// Converge builds for it, carrying its bytes over stdin, naming the mode and
// the owner the file is to carry once in place.
//
// The write is recognized by rebuilding it rather than by reading its shell
// text, so the remote recipe can change without this changing with it. The
// mode and the owner are looked for as the values themselves, because a recipe
// that dropped one would rebuild identically for every file and so would go
// unseen.
func (f *fakeBox) wrote(file File) bool {
	for _, w := range f.writes {
		if w.cmd == writeCommand(file) && w.bytes == string(file.Bytes) {
			return names(w.cmd, file.Mode, file.Owner)
		}
	}
	return false
}

// made reports whether the box was told to hold exactly this directory, with
// the mode and the owner it is to carry.
func (f *fakeBox) made(d Dir) bool {
	return f.ran(dirCommand(d)) && names(dirCommand(d), d.Mode, d.Owner)
}

// names reports whether the command carries the mode and every part of the
// owner, however the recipe happens to spell them.
func names(cmd, mode, owner string) bool {
	if !strings.Contains(cmd, mode) {
		return false
	}
	for _, part := range strings.Split(owner, ":") {
		if !strings.Contains(cmd, part) {
			return false
		}
	}
	return true
}

// deleted reports whether the box was told to prune the path.
func (f *fakeBox) deleted(path string) bool {
	return f.ran(pruneCommand(path))
}

// ran reports whether the box was handed the command.
func (f *fakeBox) ran(want string) bool {
	for _, cmd := range f.commands {
		if cmd == want {
			return true
		}
	}
	return false
}

// stdin is every value the box was handed over stdin, in the order it was.
func (f *fakeBox) stdin() []string {
	values := make([]string, 0, len(f.writes))
	for _, w := range f.writes {
		values = append(values, w.bytes)
	}
	return values
}

// changeOf is what the run reported it did to the file at path, and whether it
// reported that file at all.
func changeOf(r Result, path string) (Change, bool) {
	for _, e := range r.Entries {
		if e.Path == path {
			return e.Change, true
		}
	}
	return Unchanged, false
}

func TestConvergeStagesTheDocumentByteForByte(t *testing.T) {
	document := "access: tailscale\nprovider:\n  create: [hcloud]\n"
	tree := Plan([]byte(document), blueprint.Blueprint{})
	box := &fakeBox{}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !box.wrote(tree.Document) {
		t.Errorf("the box was handed %q, want the document byte for byte", box.stdin())
	}
	if change, ok := changeOf(result, DocumentPath); !ok || change != Staged {
		t.Errorf("Result reported %s as %v, want it staged", DocumentPath, change)
	}
}

func TestConvergePutsTheDocumentInPlaceRootOwnedAt0644(t *testing.T) {
	document := "access: public\n"
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte(document), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	want := File{Path: DocumentPath, Bytes: []byte(document), Mode: "0644", Owner: "root:root"}
	if !box.wrote(want) {
		t.Errorf("the document was not put in place root-owned at 0644:\n%s", strings.Join(box.commands, "\n"))
	}
}

func TestConvergeLeavesAnUnchangedDocumentAlone(t *testing.T) {
	document := "access: public\n"
	tree := resolvedTree(t, blueprint.Blueprint{})
	box := &fakeBox{files: map[string]string{
		DocumentPath: document,
		EnvPath:      string(tree.Env.File.Bytes),
	}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.writes) != 0 {
		t.Errorf("an unchanged box was written %q, want no write at all", box.stdin())
	}
	if change, ok := changeOf(result, DocumentPath); !ok || change != Unchanged {
		t.Errorf("Result reported %s as %v, want it unchanged", DocumentPath, change)
	}
}

func TestConvergeReplacesAnEditedDocumentWholesale(t *testing.T) {
	staged := "access: public\nrepos:\n  - name: api\n"
	edited := "access: public\n"
	tree := resolvedTree(t, blueprint.Blueprint{})
	box := &fakeBox{files: map[string]string{
		DocumentPath: staged,
		EnvPath:      string(tree.Env.File.Bytes),
	}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if got := box.stdin(); len(got) != 1 || got[0] != edited {
		t.Fatalf("the box was handed %q, want the edited document alone, whole", got)
	}
	if !box.wrote(tree.Document) {
		t.Errorf("the edited document was not put in place:\n%s", strings.Join(box.commands, "\n"))
	}
	if change, ok := changeOf(result, DocumentPath); !ok || change != Updated {
		t.Errorf("Result reported %s as %v, want it updated", DocumentPath, change)
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
	const credential = "s3cr3t-token"
	t.Setenv("NPM_TOKEN", credential)
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "~/.npmrc", Perms: "0640"}}}
	tree, err := Resolve(Plan([]byte("access: public\n"), b), secret.Resolve, blueprint.Value)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	box := &fakeBox{}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	key := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	want := File{Path: key, Bytes: []byte(credential), Mode: "0640", Owner: "smith:smith"}
	if !box.wrote(want) {
		t.Errorf("the placement was not staged at %s with its declared permissions", key)
	}
	if change, ok := changeOf(result, key); !ok || change != Staged {
		t.Errorf("Result reported %s as %v, want it staged", key, change)
	}
}

func TestConvergeDeliversPlacementBytesOverStdinNotArgv(t *testing.T) {
	const credential = "s3cr3t-token"
	t.Setenv("NPM_TOKEN", credential)
	b := blueprint.Blueprint{Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "/home/smith/.npmrc"}}}
	tree, err := Resolve(Plan([]byte("access: public\n"), b), secret.Resolve, blueprint.Value)
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
	if !delivered(box.stdin(), credential) {
		t.Errorf("stdin carried %q, want the credential among it", box.stdin())
	}
}

// delivered reports whether the box was handed the value over stdin.
func delivered(stdin []string, value string) bool {
	for _, in := range stdin {
		if in == value {
			return true
		}
	}
	return false
}

func TestConvergeMakesThePlacementsDirectorySmithOwnedAt0700(t *testing.T) {
	box := &fakeBox{}
	if _, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{})); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !box.made(Dir{Path: PlacementsDir, Mode: "0700", Owner: "smith:smith"}) {
		t.Errorf("commands do not make %s smith-owned at 0700:\n%s", PlacementsDir, strings.Join(box.commands, "\n"))
	}
}

// TestConvergeStagesNoPlacementBytesWhenNoneAreDeclared proves a blueprint
// declaring no placement stages none: what reaches the box is the document and
// the env beside it, which every box is owed, and nothing more.
func TestConvergeStagesNoPlacementBytesWhenNoneAreDeclared(t *testing.T) {
	box := &fakeBox{}
	result, err := Converge(context.Background(), box, resolvedTree(t, blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if len(box.writes) != 2 {
		t.Errorf("the box was handed %q, want the document and the env alone", box.stdin())
	}
	var staged []string
	for _, e := range result.Entries {
		staged = append(staged, e.Path)
	}
	if len(staged) != 2 || staged[0] != DocumentPath || staged[1] != EnvPath {
		t.Errorf("Result covered %v, want the document and the env alone", result.Entries)
	}
}

// resolvedTree plans a blueprint and attaches canned bytes to every placement,
// standing in for sources that resolve only on the operator's machine.
func resolvedTree(t *testing.T, b blueprint.Blueprint) Tree {
	t.Helper()
	canned := func(ref string) (string, error) { return "bytes of " + ref, nil }
	tree, err := Resolve(Plan([]byte("access: public\n"), b), canned, canned)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return tree
}

func TestConvergePrunesAPlacementRemovedFromTheBlueprint(t *testing.T) {
	kept := BoxPlacementPathIn(Root, "/home/smith/.gitconfig")
	removed := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	tree := resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:GIT_CONFIG", To: "/home/smith/.gitconfig"},
	}})
	box := &fakeBox{tree: []string{boxDirIn(Root), kept, removed}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !box.deleted(removed) {
		t.Errorf("the removed placement was not deleted:\n%s", strings.Join(box.commands, "\n"))
	}
	if box.deleted(kept) {
		t.Errorf("a declared placement was deleted:\n%s", strings.Join(box.commands, "\n"))
	}
	if change, ok := changeOf(result, removed); !ok || change != Pruned {
		t.Errorf("Result reported %s as %v, want it pruned", removed, change)
	}
}

func TestConvergePrunesTheStagedDirectoryOfARemovedRepo(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "web", Placements: []blueprint.Placement{{From: "env:WEB_ENV", To: ".env"}}},
	}})
	gone := RepoPlacementPathIn(Root, "api", ".env")
	repoDir := filepath.Join(repoDirIn(Root), "api")
	box := &fakeBox{tree: []string{
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
	if !box.deleted(repoDir) {
		t.Errorf("the removed repo's staged directory survived:\n%s", strings.Join(box.commands, "\n"))
	}
	if box.deleted(gone) {
		t.Errorf("a file inside a pruned directory was deleted separately:\n%s", strings.Join(box.commands, "\n"))
	}
	if pruned(result) != 1 {
		t.Errorf("Result reported %v, want the pruned repo reported once", result.Entries)
	}
}

// pruned counts the files the run reported it deleted.
func pruned(r Result) int {
	var n int
	for _, e := range r.Entries {
		if e.Change == Pruned {
			n++
		}
	}
	return n
}

func TestConvergePrunesAStrayFileLeftInTheStagedTree(t *testing.T) {
	stray := filepath.Join(PlacementsDir, "left-by-a-human")
	box := &fakeBox{tree: []string{stray}}

	result, err := Converge(context.Background(), box, Plan([]byte("access: public\n"), blueprint.Blueprint{}))
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !box.deleted(stray) {
		t.Errorf("the stray file survived:\n%s", strings.Join(box.commands, "\n"))
	}
	if change, ok := changeOf(result, stray); !ok || change != Pruned {
		t.Errorf("Result reported %s as %v, want it pruned", stray, change)
	}
}

func TestConvergeReportsAChangedSourceAsUpdated(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "api", Placements: []blueprint.Placement{{From: "env:API_ENV", To: ".env"}}},
	}})
	key := RepoPlacementPathIn(Root, "api", ".env")
	box := &fakeBox{
		files: map[string]string{key: "the old bytes"},
		tree:  []string{key},
	}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if change, ok := changeOf(result, key); !ok || change != Updated {
		t.Errorf("Result reported %s as %v, want it updated", key, change)
	}
	if !box.wrote(tree.Placements[0].File) {
		t.Errorf("the box was handed %q, want the new bytes among it", box.stdin())
	}
}

func TestConvergeLeavesAnUnchangedPlacementAlone(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:NPM_TOKEN", To: "~/.npmrc"},
	}})
	key := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	box := &fakeBox{
		files: map[string]string{key: "bytes of env:NPM_TOKEN"},
		tree:  []string{boxDirIn(Root), key},
	}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if box.wrote(tree.Placements[0].File) {
		t.Errorf("an unchanged placement was rewritten with %q", box.stdin())
	}
	if box.deleted(key) {
		t.Errorf("a declared placement was pruned:\n%s", strings.Join(box.commands, "\n"))
	}
	if change, ok := changeOf(result, key); !ok || change != Unchanged {
		t.Errorf("Result reported %s as %v, want it unchanged", key, change)
	}
}

func TestConvergeTouchesNothingOutsideTheBoxStateDirectory(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{
		Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "/home/smith/.npmrc"}},
		Repos: []blueprint.Repo{
			{Name: "api", Placements: []blueprint.Placement{{From: "env:API_ENV", To: ".env"}}},
		},
	})
	box := &fakeBox{tree: []string{filepath.Join(PlacementsDir, "stray")}}

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
