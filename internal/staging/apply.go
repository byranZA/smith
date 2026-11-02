package staging

import (
	"crypto/sha256"
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

// Owner gives a path the user and group an owner string, "user:group", names.
// It is injected into Apply so tests run unprivileged.
type Owner func(path, owner string) error

// ChownByName is the Owner a box uses: it resolves both names and chowns.
func ChownByName(path, owner string) error {
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
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

// Apply writes the bundle under root, which is Root on a box and a temporary
// directory in a test, and prints one report line per file to report as each
// lands. It runs as root on the box, so the writes are Go calls, not commands.
//
// It is check-before-change: a file whose digest already matches is left
// alone, so a re-run of an unchanged blueprint rewrites nothing and moves no
// modification time. Absence is a stat result: a destination that does not
// exist is staged, and any other failure to read it stops the run.
//
// Each file is written to a temporary path beside its destination, given its
// mode and owner there, and renamed into place, so a destination is wholly old
// or wholly new and a secret never exists with a wider mode. The run stops at
// the first failure, names the path, and prunes nothing. Prune runs only after
// every write succeeded, and a walk of the placements directory that fails is
// an error, never an empty listing.
func Apply(root string, b Bundle, own Owner, report io.Writer) (Result, error) {
	var result Result
	for _, d := range b.Dirs {
		if err := ensureDir(root, d, own); err != nil {
			return result, err
		}
	}
	for _, f := range b.Files {
		change, err := stageFile(root, f, own)
		if err != nil {
			return result, err
		}
		if err := note(&result, report, Entry{Path: f.Path, Change: change}); err != nil {
			return result, err
		}
	}
	pruned, err := prune(root, b, report)
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

// onDisk maps a path on the box to where it sits under root.
func onDisk(root, boxPath string) string {
	return filepath.Join(root, strings.TrimPrefix(boxPath, Root))
}

// ensureDir makes the directory exist and applies its mode and owner every
// time, so one a human has widened is narrowed back.
func ensureDir(root string, d Dir, own Owner) error {
	p := onDisk(root, d.Path)
	mode, err := strconv.ParseUint(d.Mode, 8, 32)
	if err != nil {
		return fmt.Errorf("make %s: mode %q is not octal", d.Path, d.Mode)
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	if err := os.Chmod(p, fs.FileMode(mode)); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	if err := own(p, d.Owner); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	return nil
}

// stageFile stages one file, skipping the write when the box holds its bytes.
func stageFile(root string, f File, own Owner) (change Change, err error) {
	dest := onDisk(root, f.Path)
	held, err := os.ReadFile(dest)
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
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".staging-*")
	if err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(f.Bytes); err != nil {
		_ = tmp.Close()
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = tmp.Close(); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = os.Chmod(tmp.Name(), fs.FileMode(mode)); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = own(tmp.Name(), f.Owner); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if err = os.Rename(tmp.Name(), dest); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	return change, nil
}

// prune deletes everything under the placements directory the bundle does not
// declare, and reports each deletion.
//
// The walk visits parents before their children, and a path inside one already
// deleted is skipped, so a repo dropped from the blueprint is reported as the
// one directory it is rather than as every file it happened to hold.
func prune(root string, b Bundle, report io.Writer) ([]Entry, error) {
	top := onDisk(root, PlacementsDir)
	declared := make(map[string]bool, len(b.Dirs)+len(b.Files))
	for _, d := range b.Dirs {
		declared[onDisk(root, d.Path)] = true
	}
	for _, f := range b.Files {
		declared[onDisk(root, f.Path)] = true
	}

	var held []string
	err := filepath.WalkDir(top, func(p string, _ fs.DirEntry, err error) error {
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
		if declared[p] || within(p, deleted) {
			continue
		}
		boxPath := filepath.Join(Root, strings.TrimPrefix(p, filepath.Clean(root)))
		if err := os.RemoveAll(p); err != nil {
			return result.Entries, fmt.Errorf("prune %s: %w", boxPath, err)
		}
		deleted = append(deleted, p)
		if err := note(&result, report, Entry{Path: boxPath, Change: Pruned}); err != nil {
			return result.Entries, err
		}
	}
	return result.Entries, nil
}

// within reports whether path sits inside one of the given directories.
func within(path string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
