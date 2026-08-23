package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the root every declared repo occupies a directory under, and
// the names the blueprint still declares there. It is what the orphans scan is
// read against: a directory holding a clone that no declared name matches is a
// repo the operator dropped.
type Workspace struct {
	// Root is the workspace root on the box, absolute.
	Root string
	// Declared are the repo names the blueprint declares under the root, each
	// already defaulted from its url where the operator named none.
	Declared []string
}

// reportOrphans names the repos the box holds that the blueprint no longer
// declares, and leaves every one of them exactly where it is.
//
// This is deliberately the opposite of the staged config tree's prune rule.
// That tree holds bytes smith wrote and can write again; this one holds bare
// repos, their worktrees, and work that may be pushed nowhere. Reclaiming any
// of it unasked clears a far higher bar than a verb the operator runs
// themselves (docs/adr/0010), so the scan reports and never deletes.
func reportOrphans(w Workspace) (string, error) {
	orphans, err := orphaned(w)
	if err != nil {
		return "", err
	}
	if len(orphans) == 0 {
		return "no orphans under " + w.Root, nil
	}
	return fmt.Sprintf("orphans left in place under %s: %s", w.Root, strings.Join(orphans, ", ")), nil
}

// orphaned is the repos under the root the blueprint no longer declares, in
// the order the box holds them so the report reads the same from one run to
// the next.
//
// A root the box does not have yet holds nothing, which is the first-run case
// rather than a failure. Only a directory holding a bare clone counts: a
// directory the operator made under the root themselves is not a repo smith
// ever managed, so it is neither reported nor its business.
func orphaned(w Workspace) ([]string, error) {
	entries, err := os.ReadDir(w.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the repos the box holds under %s: %w", w.Root, err)
	}
	declared := make(map[string]bool, len(w.Declared))
	for _, name := range w.Declared {
		declared[name] = true
	}
	var orphans []string
	for _, entry := range entries {
		if !entry.IsDir() || declared[entry.Name()] {
			continue
		}
		if holdsClone(filepath.Join(w.Root, entry.Name())) {
			orphans = append(orphans, entry.Name())
		}
	}
	return orphans, nil
}

// holdsClone reports whether a directory under the workspace root holds a bare
// clone, which is what makes it a repo rather than somewhere the operator
// keeps something of their own.
func holdsClone(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, cloneDir))
	return err == nil && info.IsDir()
}
