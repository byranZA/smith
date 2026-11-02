package staging

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/blueprint"
)

// owners records the owner each path was given, standing in for the root-only
// chown so the apply runs unprivileged.
type owners map[string]string

func (o owners) own(path, owner string) error {
	// A file is owned while still at its temporary name beside the destination.
	dest, _, _ := strings.Cut(path, ".staging-")
	o[dest] = owner
	return nil
}

// disk is a real temporary directory standing in for /etc/smith.
type disk struct {
	t      *testing.T
	root   string
	owners owners
}

func newDisk(t *testing.T) *disk {
	t.Helper()
	return &disk{t: t, root: t.TempDir(), owners: owners{}}
}

// path is where a box path sits on the temporary disk.
func (d *disk) path(boxPath string) string { return onDisk(d.root, boxPath) }

// apply runs Apply for the tree and returns what it reported on the way.
func (d *disk) apply(tree Tree) (Result, string, error) {
	d.t.Helper()
	var report strings.Builder
	result, err := Apply(d.root, BundleOf(tree), d.owners.own, &report)
	return result, report.String(), err
}

// mustApply is apply for a run that is expected to succeed.
func (d *disk) mustApply(tree Tree) Result {
	d.t.Helper()
	result, _, err := d.apply(tree)
	if err != nil {
		d.t.Fatalf("Apply() error = %v", err)
	}
	return result
}

// read is the bytes held at a box path.
func (d *disk) read(boxPath string) string {
	d.t.Helper()
	data, err := os.ReadFile(d.path(boxPath))
	if err != nil {
		d.t.Fatalf("read %s: %v", boxPath, err)
	}
	return string(data)
}

// seed writes bytes at a box path, creating its directory.
func (d *disk) seed(boxPath, content string) {
	d.t.Helper()
	p := d.path(boxPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		d.t.Fatal(err)
	}
}

// exists reports whether anything is held at a box path.
func (d *disk) exists(boxPath string) bool {
	_, err := os.Lstat(d.path(boxPath))
	return err == nil
}

// mode is the permissions held at a box path.
func (d *disk) mode(boxPath string) os.FileMode {
	d.t.Helper()
	info, err := os.Stat(d.path(boxPath))
	if err != nil {
		d.t.Fatal(err)
	}
	return info.Mode().Perm()
}

// changeOf is what the run reported it did to the file at path.
func changeOf(r Result, path string) (Change, bool) {
	for _, e := range r.Entries {
		if e.Path == path {
			return e.Change, true
		}
	}
	return Unchanged, false
}

// resolvedTree plans a blueprint and attaches canned bytes to every placement,
// standing in for sources that resolve only on the operator's machine.
func resolvedTree(t *testing.T, b blueprint.Blueprint) Tree {
	t.Helper()
	canned := func(ref string) (string, error) { return "bytes of " + ref, nil }
	tree, err := Resolve(Plan([]byte("access: public\n"), b, Resolution{}), canned, canned)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return tree
}

func npmrcTree(t *testing.T) Tree {
	return resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:NPM_TOKEN", To: "/home/smith/.npmrc", Perms: "0640"},
	}})
}

func TestApplyStagesTheDocumentByteForByteRootOwnedAt0644(t *testing.T) {
	d := newDisk(t)
	const document = "# acme\naccess: tailscale\n"

	result := d.mustApply(Plan([]byte(document), blueprint.Blueprint{}, Resolution{}))

	if got := d.read(DocumentPath); got != document {
		t.Errorf("staged document = %q, want %q", got, document)
	}
	if got := d.mode(DocumentPath); got != 0o644 {
		t.Errorf("document mode = %v, want 0644", got)
	}
	if got := d.owners[d.path(DocumentPath)]; got != "root:root" {
		t.Errorf("document owner = %q, want root:root", got)
	}
	if change, ok := changeOf(result, DocumentPath); !ok || change != Staged {
		t.Errorf("Result reported the document as %v, want staged", change)
	}
}

func TestApplyStagesAPlacementSmithOwnedWithItsDeclaredMode(t *testing.T) {
	d := newDisk(t)
	tree := npmrcTree(t)
	path := BoxPlacementPathIn(Root, "/home/smith/.npmrc")

	d.mustApply(tree)

	if got := d.read(path); got != "bytes of env:NPM_TOKEN" {
		t.Errorf("staged placement = %q, want the resolved bytes", got)
	}
	if got := d.mode(path); got != 0o640 {
		t.Errorf("placement mode = %v, want 0640", got)
	}
	if got := d.owners[d.path(path)]; got != "smith:smith" {
		t.Errorf("placement owner = %q, want smith:smith", got)
	}
}

func TestApplyEnsuresTheDirectoriesAndNarrowsAWidenedOne(t *testing.T) {
	d := newDisk(t)
	if err := os.MkdirAll(d.path(PlacementsDir), 0o755); err != nil {
		t.Fatal(err)
	}

	d.mustApply(Plan([]byte("access: public\n"), blueprint.Blueprint{}, Resolution{}))

	if got := d.mode(PlacementsDir); got != 0o700 {
		t.Errorf("placements dir mode = %v, want 0700", got)
	}
	if got := d.owners[d.path(PlacementsDir)]; got != "smith:smith" {
		t.Errorf("placements dir owner = %q, want smith:smith", got)
	}
}

func TestApplyReportsOneLinePerFileAsItLands(t *testing.T) {
	d := newDisk(t)

	_, report, err := d.apply(npmrcTree(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{DocumentPath, EnvPath, ResolutionPath, BoxPlacementPathIn(Root, "/home/smith/.npmrc")} {
		if strings.Count(report, want) != 1 || !strings.Contains(report, "staged") {
			t.Errorf("report = %q, want one staged line naming %s", report, want)
		}
	}
}

func TestApplyReapplyingAnUnchangedTreeChangesNothing(t *testing.T) {
	d := newDisk(t)
	tree := npmrcTree(t)
	d.mustApply(tree)
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	before := map[string]time.Time{}
	for _, f := range BundleOf(tree).Files {
		if err := os.Chtimes(d.path(f.Path), old, old); err != nil {
			t.Fatal(err)
		}
		before[f.Path] = old
	}

	result := d.mustApply(tree)

	for _, e := range result.Entries {
		if e.Change != Unchanged {
			t.Errorf("Result reported %s as %v, want every entry unchanged", e.Path, e.Change)
		}
	}
	for path, mtime := range before {
		info, err := os.Stat(d.path(path))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(mtime) {
			t.Errorf("%s mtime moved to %v, want it left at %v", path, info.ModTime(), mtime)
		}
	}
}

func TestApplyReplacesAnEditedFileWholesaleAndReportsUpdated(t *testing.T) {
	d := newDisk(t)
	d.seed(DocumentPath, "access: public\nterminal: zellij\n")

	result := d.mustApply(Plan([]byte("access: tailscale\n"), blueprint.Blueprint{}, Resolution{}))

	if got := d.read(DocumentPath); got != "access: tailscale\n" {
		t.Errorf("document = %q, want it replaced whole", got)
	}
	if change, _ := changeOf(result, DocumentPath); change != Updated {
		t.Errorf("Result reported the document as %v, want updated", change)
	}
}

func TestApplyLeavesNoTemporaryFileBehind(t *testing.T) {
	d := newDisk(t)

	d.mustApply(npmrcTree(t))

	entries, err := os.ReadDir(d.path(Root))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".staging") {
			t.Errorf("%s left behind", e.Name())
		}
	}
}

func TestApplyCreatesNothingOutsideTheStateDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "etc-smith")

	if _, err := Apply(root, BundleOf(npmrcTree(t)), owners{}.own, &strings.Builder{}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "etc-smith" {
		t.Errorf("parent holds %v, want only the state directory", entries)
	}
}

func TestApplyPrunesAPlacementRemovedFromTheBlueprint(t *testing.T) {
	d := newDisk(t)
	kept := BoxPlacementPathIn(Root, "/home/smith/.gitconfig")
	removed := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	d.seed(removed, "old secret")
	tree := resolvedTree(t, blueprint.Blueprint{Placements: []blueprint.Placement{
		{From: "env:GIT_CONFIG", To: "/home/smith/.gitconfig"},
	}})

	result := d.mustApply(tree)

	if d.exists(removed) {
		t.Error("the removed placement survived")
	}
	if !d.exists(kept) {
		t.Error("a declared placement was deleted")
	}
	if change, ok := changeOf(result, removed); !ok || change != Pruned {
		t.Errorf("Result reported %s as %v, want it pruned", removed, change)
	}
}

func TestApplyPrunesTheStagedDirectoryOfARemovedRepoOnce(t *testing.T) {
	d := newDisk(t)
	d.seed(RepoPlacementPathIn(Root, "api", ".env"), "old")
	d.seed(RepoPlacementPathIn(Root, "api", ".other"), "old")
	tree := resolvedTree(t, blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "web", Placements: []blueprint.Placement{{From: "env:WEB_ENV", To: ".env"}}},
	}})

	result := d.mustApply(tree)

	repoDir := filepath.Join(repoDirIn(Root), "api")
	if d.exists(repoDir) {
		t.Error("the removed repo's staged directory survived")
	}
	var pruned []string
	for _, e := range result.Entries {
		if e.Change == Pruned {
			pruned = append(pruned, e.Path)
		}
	}
	if !reflect.DeepEqual(pruned, []string{repoDir}) {
		t.Errorf("pruned %v, want the repo directory once", pruned)
	}
	if !d.exists(RepoPlacementPathIn(Root, "web", ".env")) {
		t.Error("a declared repo placement was deleted")
	}
}

func TestApplyPrunesAStrayFileLeftInTheStagedTree(t *testing.T) {
	d := newDisk(t)
	stray := filepath.Join(PlacementsDir, "left-by-a-human")
	d.seed(stray, "x")

	d.mustApply(Plan([]byte("access: public\n"), blueprint.Blueprint{}, Resolution{}))

	if d.exists(stray) {
		t.Error("the stray file survived")
	}
}

// A destination that cannot be read for any reason but its absence is an
// error: here a directory sits where the resolution belongs. The run stops at
// that file, so the files before it hold new bytes, the ones after hold old
// bytes, and nothing is pruned.
func TestApplyStopsAtTheFirstFailureLeavingEarlierNewLaterOldAndNothingPruned(t *testing.T) {
	d := newDisk(t)
	placement := BoxPlacementPathIn(Root, "/home/smith/.npmrc")
	stray := filepath.Join(PlacementsDir, "stray")
	d.seed(placement, "old placement")
	d.seed(stray, "x")
	if err := os.MkdirAll(d.path(ResolutionPath), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := d.apply(npmrcTree(t))

	if err == nil {
		t.Fatal("Apply() succeeded, want the unreadable destination to fail the run")
	}
	if !strings.Contains(err.Error(), ResolutionPath) {
		t.Errorf("Apply() error = %v, want it to name %s", err, ResolutionPath)
	}
	if got := d.read(DocumentPath); got != "access: public\n" {
		t.Errorf("document = %q, want the new bytes", got)
	}
	if got := d.read(EnvPath); got == "old" {
		t.Errorf("env = %q, want the new bytes", got)
	}
	if got := d.read(placement); got != "old placement" {
		t.Errorf("later placement = %q, want its old bytes", got)
	}
	if !d.exists(stray) {
		t.Error("the run pruned before every write succeeded")
	}
}

func TestApplyFailsWhenThePlacementsDirectoryCannotBeWalked(t *testing.T) {
	d := newDisk(t)

	_, err := Apply(d.root, Bundle{}, d.owners.own, &strings.Builder{})

	if err == nil {
		t.Fatal("Apply() succeeded, want an unwalkable placements directory to be an error, not an empty listing")
	}
	if !strings.Contains(err.Error(), PlacementsDir) {
		t.Errorf("Apply() error = %v, want it to name %s", err, PlacementsDir)
	}
}

func TestApplyFailsWhenTheOwnerStepFails(t *testing.T) {
	d := newDisk(t)
	boom := errors.New("no such user")

	_, err := Apply(d.root, BundleOf(npmrcTree(t)), func(string, string) error { return boom }, &strings.Builder{})

	if !errors.Is(err, boom) {
		t.Errorf("Apply() error = %v, want it to wrap %v", err, boom)
	}
}

func TestEncodeThenDecodeReturnsTheTree(t *testing.T) {
	tree := resolvedTree(t, blueprint.Blueprint{
		Placements: []blueprint.Placement{{From: "env:NPM_TOKEN", To: "~/.npmrc", Perms: "0640"}},
		Repos:      []blueprint.Repo{{Name: "api", Placements: []blueprint.Placement{{From: "env:API", To: ".env"}}}},
	})

	stream, err := Encode(tree)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := Decode(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if want := BundleOf(tree); !reflect.DeepEqual(got, want) {
		t.Errorf("Decode() = %+v, want %+v", got, want)
	}
}

func tarOf(t *testing.T, headers ...tar.Header) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range headers {
		h := h
		if h.Typeflag == tar.TypeReg {
			h.Size = 1
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestDecodeRefusesAStreamThatReachesOutsideTheStateDirectory(t *testing.T) {
	good := tar.Header{Typeflag: tar.TypeReg, Name: DocumentPath, Mode: 0o644}
	tests := map[string]tar.Header{
		"a path outside /etc/smith":    {Typeflag: tar.TypeReg, Name: "/etc/passwd", Mode: 0o644},
		"a sibling sharing the prefix": {Typeflag: tar.TypeReg, Name: "/etc/smithy/x", Mode: 0o644},
		"a .. element":                 {Typeflag: tar.TypeReg, Name: Root + "/../passwd", Mode: 0o644},
		"a relative path":              {Typeflag: tar.TypeReg, Name: "etc/smith/x", Mode: 0o644},
		"a symlink":                    {Typeflag: tar.TypeSymlink, Name: Root + "/link", Linkname: "/etc/shadow"},
		"a hardlink":                   {Typeflag: tar.TypeLink, Name: Root + "/link", Linkname: "/etc/shadow"},
		"a device":                     {Typeflag: tar.TypeChar, Name: Root + "/dev"},
		"an unclean path":              {Typeflag: tar.TypeReg, Name: Root + "//x", Mode: 0o644},
	}
	for name, bad := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(tarOf(t, good, bad)); err == nil {
				t.Errorf("Decode() succeeded, want %s refused", name)
			}
		})
	}
}

func TestDecodeRefusalMeansNothingWasWritten(t *testing.T) {
	d := newDisk(t)
	stream := tarOf(t,
		tar.Header{Typeflag: tar.TypeReg, Name: DocumentPath, Mode: 0o644},
		tar.Header{Typeflag: tar.TypeSymlink, Name: Root + "/link", Linkname: "/etc/shadow"},
	)

	if _, err := Decode(stream); err == nil {
		t.Fatal("Decode() succeeded, want the symlink refused")
	}
	if entries, _ := os.ReadDir(d.root); len(entries) != 0 {
		t.Errorf("root holds %v, want nothing written", entries)
	}
}
