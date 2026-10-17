package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Driver is who drives a session: a human in its shell, or the loop as its
// root process. Its zero value is Human, which is what a worktree with no
// session record reads as.
type Driver int

const (
	// Human is a session whose root process is a shell for a human to work in.
	Human Driver = iota
	// Loop is a session whose root process is the loop.
	Loop
)

// String returns the word the DRIVER column shows.
func (d Driver) String() string {
	switch d {
	case Loop:
		return "loop"
	default:
		return "human"
	}
}

// recordFile is the session record's file name in a worktree's own git dir.
const recordFile = "smith-session"

// entry is what a session leaves in its worktree's own git dir, so a stopped
// session still says who drove it and, when the loop did, how its run ended.
type entry struct {
	// Driver is "human" or "loop".
	Driver string `json:"driver"`
	// Outcome is how the loop's run ended, empty until the loop stops.
	Outcome string `json:"outcome,omitempty"`
}

// Record is the session record of one worktree, open for the loop running
// as its session's root process to record how its run ended.
type Record struct {
	path string
}

// OpenRecord opens the session record of the worktree at dir. It finds where
// the record lives now, so Conclude needs no git and still works once the
// run's context is cancelled.
func OpenRecord(ctx context.Context, git Runner, dir string) (Record, error) {
	path, err := recordPath(ctx, git, dir)
	if err != nil {
		return Record{}, err
	}
	return Record{path: path}, nil
}

// Conclude records outcome as how the loop-driven session's run ended,
// keeping the record's driver as the loop.
func (r Record) Conclude(outcome string) error {
	return writeEntry(r.path, entry{Driver: Loop.String(), Outcome: outcome})
}

// driverOf returns the driver a session started with command has: the loop
// when there is a root command, a human otherwise.
func driverOf(command []string) Driver {
	if len(command) > 0 {
		return Loop
	}
	return Human
}

// writeRecord records the driver of the session starting in the worktree at
// dir, overwriting whatever an earlier session there left.
func writeRecord(ctx context.Context, git Runner, dir string, d Driver) error {
	path, err := recordPath(ctx, git, dir)
	if err != nil {
		return err
	}
	return writeEntry(path, entry{Driver: d.String()})
}

// writeEntry replaces the session record at path with e atomically, so a
// reader sees the old record or the new one whole and a failed write keeps the
// old one.
func writeEntry(path string, e entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode the session record %s: %w", path, err)
	}
	if err := replace(path, data); err != nil {
		return fmt.Errorf("write the session record %s: %w", path, err)
	}
	return nil
}

// replace writes data over the file at path through a temp file beside it,
// renamed into place. The temp file is removed when the write fails.
func replace(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if err := writeAndClose(tmp, data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", name, err), removeTemp(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(fmt.Errorf("rename %s into place: %w", name, err), removeTemp(name))
	}
	return nil
}

// writeAndClose writes data to f, gives it the record's owner-only
// permissions, and closes it.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Chmod(0o600); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// removeTemp deletes an abandoned temp file, reporting a failure that would
// leave it behind.
func removeTemp(name string) error {
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove the temporary file %s: %w", name, err)
	}
	return nil
}

// readRecord returns the driver recorded for the worktree at dir and the
// outcome its loop recorded, empty when none was. A worktree with no record,
// one started before sessions recorded their driver, reads as Human.
func readRecord(ctx context.Context, git Runner, dir string) (Driver, string, error) {
	path, err := recordPath(ctx, git, dir)
	if err != nil {
		return Human, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Human, "", nil
	}
	if err != nil {
		return Human, "", fmt.Errorf("read the session record for %s: %w", dir, err)
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Human, "", fmt.Errorf("malformed session record %s: %w", path, err)
	}
	if e.Driver == Loop.String() {
		return Loop, e.Outcome, nil
	}
	return Human, "", nil
}

// recordPath returns where the session record of the worktree at dir lives:
// in the worktree's own git dir, so git removes it with the worktree.
func recordPath(ctx context.Context, git Runner, dir string) (string, error) {
	gitDir, err := run(ctx, git, "git", "-C", dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("find the git dir of the worktree at %s: %w", dir, err)
	}
	return filepath.Join(gitDir, recordFile), nil
}
