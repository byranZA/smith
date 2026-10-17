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

// record is what a session leaves in its worktree's own git dir, so a stopped
// session still says who drove it.
type record struct {
	// Driver is "human" or "loop".
	Driver string `json:"driver"`
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
	data, err := json.Marshal(record{Driver: d.String()})
	if err != nil {
		return fmt.Errorf("encode the session record for %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write the session record for %s: %w", dir, err)
	}
	return nil
}

// readDriver returns the driver recorded for the worktree at dir. A worktree
// with no record, one started before sessions recorded their driver, reads as
// Human.
func readDriver(ctx context.Context, git Runner, dir string) (Driver, error) {
	path, err := recordPath(ctx, git, dir)
	if err != nil {
		return Human, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Human, nil
	}
	if err != nil {
		return Human, fmt.Errorf("read the session record for %s: %w", dir, err)
	}
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return Human, fmt.Errorf("malformed session record %s: %w", path, err)
	}
	if r.Driver == Loop.String() {
		return Loop, nil
	}
	return Human, nil
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
