package staging

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Change is what applying did to one file of the staged tree.
type Change int

const (
	// Unchanged means the box already held these exact bytes and nothing was
	// written, so the file's modification time did not move.
	Unchanged Change = iota
	// Staged means the file was written for the first time: the box held
	// nothing at that path.
	Staged
	// Updated means the box held different bytes there and they were replaced,
	// which is what a placement whose source has changed reports.
	Updated
	// Pruned means the box held something under the placements tree that the
	// blueprint no longer declares, and it was deleted.
	Pruned
)

// String renders the change as the operator is told it.
func (c Change) String() string {
	switch c {
	case Staged:
		return "staged"
	case Updated:
		return "updated"
	case Pruned:
		return "pruned"
	default:
		return "unchanged"
	}
}

// Entry is one file of the staged tree and what applying did to it.
type Entry struct {
	// Path is the file's absolute path on the box.
	Path string
	// Change is what the run did to it.
	Change Change
}

// Result is what an apply run did to the staged tree, in the order it did it.
type Result struct {
	// Entries are the staged files the run considered, each with its change.
	Entries []Entry
}

// ReportHeading opens the report an apply prints.
const ReportHeading = "config staging:\n"

// Owner gives an open file or directory the user and group an owner string,
// "user:group", names. It works on the open handle so the change cannot be
// redirected, and is injected into Apply so tests run unprivileged.
type Owner func(f *os.File, owner string) error

// ChownByName is the Owner a box uses: it resolves both names and chowns.
func ChownByName(f *os.File, owner string) error {
	userName, groupName, _ := strings.Cut(owner, ":")
	u, err := user.Lookup(userName)
	if err != nil {
		return fmt.Errorf("look up user %s: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return fmt.Errorf("look up group %s: %w", groupName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("user %s has uid %q: %w", userName, u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("group %s has gid %q: %w", groupName, g.Gid, err)
	}
	if err := f.Chown(uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", f.Name(), err)
	}
	return nil
}

// Apply writes the bundle under root, leaving a file whose digest already matches untouched, and prints one report line per file to report. It stops at the first failure naming the path, and prunes only after every write succeeded.
func Apply(root string, b Bundle, own Owner, report io.Writer) (Result, error) {
	var result Result
	if err := os.MkdirAll(root, 0o700); err != nil {
		return result, fmt.Errorf("make %s: %w", root, err)
	}
	box, err := os.OpenRoot(root)
	if err != nil {
		return result, fmt.Errorf("open %s: %w", root, err)
	}
	defer func() { _ = box.Close() }()
	for _, d := range b.Dirs {
		if err := ensureDir(box, d, own); err != nil {
			return result, err
		}
	}
	for _, f := range b.Files {
		change, err := stageFile(box, f, own)
		if err != nil {
			return result, err
		}
		if err := note(&result, report, Entry{Path: f.Path, Change: change}); err != nil {
			return result, err
		}
	}
	pruned, err := prune(box, b, report)
	result.Entries = append(result.Entries, pruned...)
	return result, err
}

// note records an entry and reports it.
func note(r *Result, report io.Writer, e Entry) error {
	r.Entries = append(r.Entries, e)
	if _, err := fmt.Fprintf(report, "  %-9s %s\n", e.Change, e.Path); err != nil {
		return fmt.Errorf("write the staging report: %w", err)
	}
	return nil
}

// rootName maps a path on the box to its name inside the state root, where "."
// is the root itself.
func rootName(boxPath string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(filepath.Clean(boxPath), Root), "/")
	if rel == "" {
		return "."
	}
	return rel
}

// ensureDir makes the directory exist and applies its mode and owner every
// time, so one a human has widened is narrowed back. Every step stays inside
// the root, so a link along the way fails the run instead of being followed out.
func ensureDir(box *os.Root, d Dir, own Owner) error {
	mode, err := strconv.ParseUint(d.Mode, 8, 32)
	if err != nil {
		return fmt.Errorf("make %s: mode %q is not octal", d.Path, d.Mode)
	}
	name := rootName(d.Path)
	if err := box.MkdirAll(name, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	dir, err := box.Open(name)
	if err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Chmod(fs.FileMode(mode)); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	if err := own(dir, d.Owner); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	return nil
}

// tempFile creates a new file beside name inside the root, failing rather than
// reusing a name that is taken.
func tempFile(box *os.Root, name string) (*os.File, error) {
	for range 100 {
		var salt [6]byte
		if _, err := rand.Read(salt[:]); err != nil {
			return nil, fmt.Errorf("name a temporary file: %w", err)
		}
		f, err := box.OpenFile(name+".staging-"+hex.EncodeToString(salt[:]), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create a temporary file beside %s: %w", name, err)
		}
		if err == nil {
			return f, nil
		}
	}
	return nil, fmt.Errorf("no free temporary name beside %s", name)
}

// stageFile stages one file, skipping the write when the box holds its bytes.
func stageFile(box *os.Root, f File, own Owner) (change Change, err error) {
	name := rootName(f.Path)
	held, err := box.ReadFile(name)
	switch {
	case err == nil:
		if sha256.Sum256(held) == sha256.Sum256(f.Bytes) {
			return Unchanged, nil
		}
		change = Updated
	case errors.Is(err, fs.ErrNotExist):
		change = Staged
	default:
		return Unchanged, fmt.Errorf("stage %s: read it: %w", f.Path, err)
	}
	mode, err := strconv.ParseUint(f.Mode, 8, 32)
	if err != nil {
		return Unchanged, fmt.Errorf("stage %s: mode %q is not octal", f.Path, f.Mode)
	}
	tmp, err := tempFile(box, name)
	if err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	tmpName := filepath.Base(tmp.Name())
	tmpName = filepath.Join(filepath.Dir(name), tmpName)
	defer func() {
		if err != nil {
			_ = box.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(f.Bytes); err != nil {
		_ = tmp.Close()
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = tmp.Chmod(fs.FileMode(mode)); err != nil {
		_ = tmp.Close()
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = own(tmp, f.Owner); err != nil {
		_ = tmp.Close()
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = tmp.Close(); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = box.Rename(tmpName, name); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	return change, nil
}

// prune deletes everything under the placements directory the bundle does not
// declare, and reports each deletion. It walks and deletes inside the root
// only, and never follows a link.
//
// The walk visits parents before their children, and a path inside one already
// deleted is skipped, so a repo dropped from the blueprint is reported as the
// one directory it is rather than as every file it happened to hold.
func prune(box *os.Root, b Bundle, report io.Writer) ([]Entry, error) {
	top := rootName(PlacementsDir)
	declared := make(map[string]bool, len(b.Dirs)+len(b.Files))
	for _, d := range b.Dirs {
		declared[rootName(d.Path)] = true
	}
	for _, f := range b.Files {
		declared[rootName(f.Path)] = true
	}

	var held []string
	err := fs.WalkDir(box.FS(), top, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != top {
			held = append(held, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", PlacementsDir, err)
	}

	var result Result
	var deleted []string
	for _, p := range held {
		if declared[p] || inside(p, deleted) {
			continue
		}
		boxPath := filepath.Join(Root, p)
		if err := box.RemoveAll(p); err != nil {
			return result.Entries, fmt.Errorf("prune %s: %w", boxPath, err)
		}
		deleted = append(deleted, p)
		if err := note(&result, report, Entry{Path: boxPath, Change: Pruned}); err != nil {
			return result.Entries, err
		}
	}
	return result.Entries, nil
}

// inside reports whether path sits inside one of the given directories.
func inside(path string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
