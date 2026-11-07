package stack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/byranZA/smith/internal/blueprint"
)

// Record is what a lease file holds about the stack its holder brought up.
type Record struct {
	// Dir is the worktree the stack came up in.
	Dir string `json:"dir"`
	// Branch is the work branch of the run that holds the lease.
	Branch string `json:"branch"`
	// Up and Down are the commands the stack was brought up and is taken
	// down with.
	Up   string `json:"up"`
	Down string `json:"down"`
	// Started is when the lease was taken.
	Started time.Time `json:"started"`
}

// HeldError is why a lease cannot be taken: a live run holds it.
type HeldError struct {
	Record
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("the stack of this repo is in use by a run of work branch %s in worktree %s: only one run uses a repo's stack at a time, so wait for it or stop it and run again", e.Branch, e.Dir)
}

// Key names the lease of the repo with origin, which SSH and HTTPS forms of
// one origin share. A repo with no origin is named by its git directory.
func Key(origin, gitDir string) string {
	name := blueprint.Normalize(origin)
	if name == "" {
		name = "git-dir:" + gitDir
	}
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:8])
}

// path is where the lease file of key lives under state.
func path(state, key string) string {
	return filepath.Join(state, "stacks", key+".json")
}

// lease is a lease file held with an OS lock for as long as it is open.
type lease struct {
	file *os.File
	path string
}

// acquire takes the lease for key under state, creating its file, or returns
// a *HeldError naming the live holder. An unlocked record left by a dead
// holder is the new holder's to overwrite.
func acquire(state, key string) (*lease, error) {
	p := path(state, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("make the stack lease directory: %w", err)
	}
	for {
		f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open the stack lease: %w", err)
		}
		locked, err := tryLock(f)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("lock the stack lease %s: %w", p, err), closeLease(f))
		}
		if !locked {
			rec, rerr := read(f)
			if cerr := closeLease(f); rerr != nil || cerr != nil {
				return nil, errors.Join(rerr, cerr)
			}
			return nil, &HeldError{*rec}
		}
		// A holder that released may have deleted the file between our open
		// and our lock; a lock on a deleted file excludes nobody.
		onDisk, serr := os.Stat(p)
		mine, ferr := f.Stat()
		if serr == nil && ferr == nil && os.SameFile(onDisk, mine) {
			return &lease{file: f, path: p}, nil
		}
		if err := closeLease(f); err != nil {
			return nil, err
		}
	}
}

// closeLease closes a lease file, naming the lease in its error.
func closeLease(f *os.File) error {
	if err := f.Close(); err != nil {
		return fmt.Errorf("close the stack lease %s: %w", f.Name(), err)
	}
	return nil
}

// write replaces the lease's record.
func (l *lease) write(r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode the stack lease record: %w", err)
	}
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("write the stack lease record: %w", err)
	}
	if _, err := l.file.WriteAt(data, 0); err != nil {
		return fmt.Errorf("write the stack lease record: %w", err)
	}
	return nil
}

// drop deletes the lease's record, while it is still held.
func (l *lease) drop() error {
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete the stack lease record: %w", err)
	}
	return nil
}

// close frees the lease's lock.
func (l *lease) close() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("release the stack lease: %w", err)
	}
	return nil
}

// read decodes the record in f. A lease file with no record yet reads as an
// empty one.
func read(f *os.File) (*Record, error) {
	data := make([]byte, 64<<10)
	n, err := f.ReadAt(data, 0)
	if err != nil && n == 0 && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the stack lease record: %w", err)
	}
	var r Record
	if n > 0 {
		if err := json.Unmarshal(data[:n], &r); err != nil {
			return nil, fmt.Errorf("read the stack lease record: %w", err)
		}
	}
	return &r, nil
}

// Holder returns the record of the live run that holds the lease for key
// under state, or nil when none does, taking nothing. It is how a preflight
// refuses a run before it starts.
func Holder(state, key string) (rec *Record, err error) {
	f, err := os.Open(path(state, key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open the stack lease: %w", err)
	}
	defer func() { err = errors.Join(err, closeLease(f)) }()
	locked, err := tryLock(f)
	if err != nil {
		return nil, fmt.Errorf("lock the stack lease: %w", err)
	}
	if locked {
		return nil, unlock(f)
	}
	return read(f)
}

// Recover takes down a stack a dead run left behind: an unlocked lease with
// a record. It runs the recorded down in the recorded worktree, streaming its
// output to out, under the same timeout as Release, and deletes the record
// when it succeeds. It returns the record it recovered, or nil when nothing
// was left or a live run holds the lease. A failed down keeps the record for
// the next run, and a worktree that is gone clears it; both return the record
// with an error naming the worktree, the down and why. With no state home it
// does nothing.
func (s *Stack) Recover(ctx context.Context, out io.Writer) (*Record, error) {
	if s.State == "" {
		return nil, nil
	}
	if _, err := os.Stat(path(s.State, s.Key)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	l, err := acquire(s.State, s.Key)
	var busy *HeldError
	if errors.As(err, &busy) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec, err := s.recoverHeld(ctx, l, out)
	if rec == nil && err == nil {
		err = l.drop()
	}
	return rec, errors.Join(err, l.close())
}

// recoverHeld takes down the stack recorded in the lease l, which the caller
// holds, and deletes the record when that succeeds or the worktree is gone.
// It returns the record it recovered, or nil when it held none to take down, leaving the lease as it is.
func (s *Stack) recoverHeld(ctx context.Context, l *lease, out io.Writer) (*Record, error) {
	rec, err := read(l.file)
	if err != nil {
		return nil, err
	}
	if rec.Down == "" {
		return nil, nil
	}
	if _, serr := os.Stat(rec.Dir); serr != nil {
		gone := fmt.Errorf("the stack left in %s cannot be taken down because the worktree is gone: run %q yourself to stop it: %w", rec.Dir, rec.Down, serr)
		return rec, errors.Join(gone, l.drop())
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DownTimeout)
	defer cancel()
	if err := s.Command.Exec(ctx, rec.Dir, Down, rec.Down, out); err != nil {
		return rec, fmt.Errorf("take down the stack left in %s: %w", rec.Dir, err)
	}
	return rec, l.drop()
}
