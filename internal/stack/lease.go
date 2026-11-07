package stack

import (
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
			_ = f.Close()
			return nil, fmt.Errorf("lock the stack lease %s: %w", p, err)
		}
		if !locked {
			rec, rerr := read(f)
			_ = f.Close()
			if rerr != nil {
				return nil, rerr
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
		_ = f.Close()
	}
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
func Holder(state, key string) (*Record, error) {
	f, err := os.Open(path(state, key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open the stack lease: %w", err)
	}
	defer func() { _ = f.Close() }()
	locked, err := tryLock(f)
	if err != nil {
		return nil, fmt.Errorf("lock the stack lease: %w", err)
	}
	if locked {
		return nil, unlock(f)
	}
	return read(f)
}
