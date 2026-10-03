package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// homeMode is the permission the config home and its cache are created with.
// The home holds blueprints naming the operator's credentials and the cache
// holds every box's address, so neither is any other account's business.
const homeMode fs.FileMode = 0o700

// gitignoreFile is the ignore file inside the config home. It is the
// operator's — a config home is a directory people commit and share — so smith
// only ever appends the one line it needs.
const gitignoreFile = ".gitignore"

// gitignoreEntry is the line that keeps the cache out of version control. The
// cache holds what smith derived rather than what the operator wrote, and a
// shared repository of blueprints has no business carrying one machine's
// address book.
const gitignoreEntry = cacheDir + "/"

// EnsureHome idempotently creates the config home and its cache directory,
// and ensures the home's .gitignore keeps the cache out of version control.
// The .gitignore is the operator's, so it is only ever appended to: one that
// already ignores the cache is left byte-identical.
func EnsureHome(h Home) error {
	if err := os.MkdirAll(h.CachePath(), homeMode); err != nil {
		return fmt.Errorf("create config home %s: %w", h.path, err)
	}
	return ensureGitignore(filepath.Join(h.path, gitignoreFile))
}

// ensureGitignore appends the cache entry to the ignore file at path unless it
// is already listed, creating the file when there is none. An entry appended
// to a file with no trailing newline gets its own line, so the operator's last
// line is never silently joined to smith's.
func ensureGitignore(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	existing := string(data)
	if listed(existing) {
		return nil
	}
	addition := gitignoreEntry + "\n"
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		addition = "\n" + addition
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := f.WriteString(addition); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// listed reports whether the ignore file's content already ignores the cache.
func listed(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == gitignoreEntry {
			return true
		}
	}
	return false
}
