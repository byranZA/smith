package session

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// workState answers the work-state predicate for one worktree: whether it
// holds work that is not committed, and how many of its commits no remote has.
//
// It is defined once and has two callers — List renders it and Remove gates on
// it — which is why both verbs live in this package: two implementations of
// "is this worktree dirty" would let list say clean while rm refuses.
//
// Dirty is what `git status` reports, minus the placement paths handed in.
// Ignored files are already outside that report, and the subtraction is done
// against the blueprint's list held in memory rather than against
// repo.git/info/exclude — that block is written for the human's git status,
// and correctness here must not depend on a file an operator may edit.
//
// Unpushed counts what no remote-tracking ref reaches, rather than what the
// upstream does not: a branch smith cut has no upstream, so the upstream form
// would error on the common case instead of reporting. A detached worktree has
// no branch to count and is reported as zero, which the readout renders as not
// applicable.
func workState(ctx context.Context, git Runner, wt worktree, placements []string) (dirty bool, unpushed int, err error) {
	status, err := run(ctx, git, "git", "-C", wt.path, "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return false, 0, fmt.Errorf("read the work state of the worktree at %s: %w", wt.path, err)
	}
	dirty = holdsWork(status, placements)
	if wt.branch == "" {
		return dirty, 0, nil
	}
	counted, err := run(ctx, git, "git", "-C", wt.path, "rev-list", "--count", wt.branch, "--not", "--remotes")
	if err != nil {
		return false, 0, fmt.Errorf("count the unpushed commits of branch %q at %s: %w", wt.branch, wt.path, err)
	}
	unpushed, err = strconv.Atoi(counted)
	if err != nil {
		return false, 0, fmt.Errorf("read the unpushed commit count %q of branch %q at %s: %w", counted, wt.branch, wt.path, err)
	}
	return dirty, unpushed, nil
}

// holdsWork reports whether a `git status --porcelain -z` report names
// anything the operator wrote — every entry it lists, less the paths smith
// placed there itself.
//
// The report is read NUL-separated so a path carrying a space, a quote or a
// newline arrives as git holds it rather than as git escapes it for a human.
func holdsWork(status string, placements []string) bool {
	placed := make(map[string]bool, len(placements))
	for _, to := range placements {
		placed[normalize(to)] = true
	}
	entries := strings.Split(status, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		// An entry is "XY <path>"; anything shorter is the empty trailer.
		if len(entry) < 4 {
			continue
		}
		code, at := entry[:2], entry[3:]
		// A rename or a copy carries its source path as the next record.
		if code[0] == 'R' || code[0] == 'C' {
			i++
		}
		if !placed[normalize(at)] {
			return true
		}
	}
	return false
}

// normalize puts a path in the one spelling the subtraction compares in: slash
// separated and cleaned, so a placement written ./.env matches the .env git
// reports.
func normalize(at string) string {
	return path.Clean(filepath.ToSlash(at))
}
