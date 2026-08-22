package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/byranZA/smith/internal/connection"
)

// Conn is the narrow slice of a connection Converge needs: run a remote
// command, and run one with a value delivered over stdin. It mirrors
// bootstrap.Conn — a box is reached the same way here as everywhere else in the
// setup domain, and the two methods are all staging can do to one.
type Conn interface {
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
	RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Change is what converging did to one file of the staged tree.
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

// Entry is one file of the staged tree and what converging did to it.
type Entry struct {
	// Path is the file's absolute path on the box.
	Path string
	// Change is what the run did to it.
	Change Change
}

// Result is what a converge run did to the staged tree, in the order it did it.
type Result struct {
	// Entries are the staged files the run considered, each with its change.
	Entries []Entry
}

// Report renders what the run staged and what it left alone, one line per file.
func (r Result) Report() string {
	var b strings.Builder
	b.WriteString("config staging:\n")
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "  %-9s %s\n", e.Change, e.Path)
	}
	return b.String()
}

// Converge applies the planned tree to the box, and reports what it did.
//
// It is check-before-change, as everywhere else in the setup domain: a file
// whose on-box digest already matches the planned bytes is left alone, so a
// re-run of an unchanged blueprint rewrites nothing and moves no modification
// time. A file that differs is replaced whole — no history, no merge — because
// the blueprint is desired state and half of one describes no box.
//
// The tree's directories are ensured first, so a blueprint declaring no
// placements still leaves an empty placements directory rather than none, and a
// directory a human has widened is narrowed back.
//
// The tree is then pruned to exactly what the blueprint declares: anything
// under the placements directory that is not a currently-declared scope and
// destination is deleted and reported, so a placement an operator drops from
// their blueprint does not leave live credential bytes on the box forever.
//
// Nothing outside /etc/smith/ is touched, so a session running against a
// worktree keeps the copy it already has; a changed placement reaches that
// worktree on the next `session start`.
//
// The bytes travel over stdin and never appear in an argument, and each write
// lands on a temporary path beside its destination before it is chmod'ed,
// chown'ed and moved into place, so the destination never exists holding
// partial content or a wider mode. That matters most for a placement: its bytes
// are a provisioned secret, and argv is readable by every process on the box.
func Converge(ctx context.Context, conn Conn, tree Tree) (Result, error) {
	for _, d := range tree.Dirs {
		if err := ensure(ctx, conn, d); err != nil {
			return Result{}, err
		}
	}
	files := make([]File, 0, 1+len(tree.Placements))
	files = append(files, tree.Document)
	for _, p := range tree.Placements {
		files = append(files, p.File)
	}

	var result Result
	for _, f := range files {
		change, err := write(ctx, conn, f)
		if err != nil {
			return Result{}, err
		}
		result.Entries = append(result.Entries, Entry{Path: f.Path, Change: change})
	}

	pruned, err := prune(ctx, conn, tree)
	if err != nil {
		return Result{}, err
	}
	result.Entries = append(result.Entries, pruned...)
	return result, nil
}

// prune deletes everything under the placements directory the tree does not
// declare, and reports each deletion.
//
// Parents are considered before their children, and a path inside one already
// deleted is skipped, so a repo dropped from the blueprint is reported as the
// one directory it is rather than as every file it happened to hold.
func prune(ctx context.Context, conn Conn, tree Tree) ([]Entry, error) {
	staged, err := stagedPaths(ctx, conn)
	if err != nil {
		return nil, err
	}
	declared := declaredPaths(tree)

	var entries []Entry
	var deleted []string
	for _, path := range staged {
		if declared[path] || within(path, deleted) {
			continue
		}
		if err := conn.Run(ctx, pruneCommand(path), io.Discard, io.Discard); err != nil {
			return nil, fmt.Errorf("prune %s: %w", path, err)
		}
		deleted = append(deleted, path)
		entries = append(entries, Entry{Path: path, Change: Pruned})
	}
	return entries, nil
}

// stagedPaths reads everything the box currently holds under the placements
// directory, outermost first. A directory that does not exist yet lists as
// nothing rather than failing, which is the ordinary first-run case.
func stagedPaths(ctx context.Context, conn Conn) ([]string, error) {
	var out bytes.Buffer
	if err := conn.Run(ctx, listCommand(), &out, io.Discard); err != nil {
		return nil, fmt.Errorf("list %s: %w", PlacementsDir, err)
	}
	var paths []string
	for _, line := range strings.Split(out.String(), "\n") {
		if path := strings.TrimSpace(line); path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// declaredPaths is every path under the placements directory the tree accounts
// for: its scope directories and the staged file of each declared placement.
func declaredPaths(tree Tree) map[string]bool {
	declared := make(map[string]bool, len(tree.Dirs)+len(tree.Placements))
	for _, d := range tree.Dirs {
		declared[d.Path] = true
	}
	for _, p := range tree.Placements {
		declared[p.File.Path] = true
	}
	return declared
}

// within reports whether path sits inside one of the given directories.
func within(path string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

// ensure makes one directory of the staged tree exist with the mode and owner
// it is to carry. It runs unconditionally, so a blueprint declaring no
// placements still leaves an empty placements directory behind rather than
// none, and a directory that already exists keeps whatever it holds.
func ensure(ctx context.Context, conn Conn, d Dir) error {
	if err := conn.Run(ctx, dirCommand(d), io.Discard, io.Discard); err != nil {
		return fmt.Errorf("make %s: %w", d.Path, err)
	}
	return nil
}

// write stages one file, skipping the write when the box already holds its
// bytes.
func write(ctx context.Context, conn Conn, f File) (Change, error) {
	staged, err := digest(ctx, conn, f.Path)
	if err != nil {
		return Unchanged, err
	}
	if staged == sum(f.Bytes) {
		return Unchanged, nil
	}
	if err := conn.RunWithInput(ctx, writeCommand(f), bytes.NewReader(f.Bytes), io.Discard, io.Discard); err != nil {
		return Unchanged, fmt.Errorf("stage %s: %w", f.Path, err)
	}
	if staged == "" {
		return Staged, nil
	}
	return Updated, nil
}

// digest reads the sha256 of the file already staged at path, or "" when the
// box holds nothing there. An absent file is the ordinary first-run case, not a
// failure, so the probe reports it as an empty digest rather than an error.
func digest(ctx context.Context, conn Conn, path string) (string, error) {
	var out bytes.Buffer
	if err := conn.Run(ctx, digestCommand(path), &out, io.Discard); err != nil {
		return "", fmt.Errorf("read staged digest %s: %w", path, err)
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(out.String()), " ", 2)[0]), nil
}

// sum is the digest of bytes as sha256sum on the box spells it.
func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// digestCommand builds the remote probe for what the box already holds at path.
// A missing file yields no output rather than a failed command.
func digestCommand(path string) string {
	return fmt.Sprintf("sudo sha256sum %s 2>/dev/null || true", connection.ShellArg(path))
}

// listCommand builds the remote listing of what the box already holds under the
// placements directory, which is what the prune is computed against. An absent
// directory yields no output rather than a failed command.
func listCommand() string {
	return fmt.Sprintf("sudo find %s -mindepth 1 2>/dev/null || true", connection.ShellArg(PlacementsDir))
}

// pruneCommand builds the remote delete of one undeclared path. It is recursive
// because the path may be a whole repo's staged directory, and forced because a
// prune of something already gone is not a failure.
func pruneCommand(path string) string {
	return fmt.Sprintf("sudo rm -rf %s", connection.ShellArg(path))
}

// dirCommand builds the remote directory ensure. install -d is idempotent: it
// creates the directory when it is absent and applies the mode and owner
// either way, so a re-run converges a directory a human has widened.
func dirCommand(d Dir) string {
	user, group, _ := strings.Cut(d.Owner, ":")
	return fmt.Sprintf("sudo install -d -m %s -o %s -g %s %s", d.Mode, user, group, connection.ShellArg(d.Path))
}

// writeCommand builds the remote write: take the bytes from stdin into a
// temporary path beside the destination — inside the directory the destination
// already sits in, so a placement's bytes never leave the 0700 placements
// directory — give the temporary file the mode and owner the destination is to
// carry, and move it into place. Every step runs through sudo, as the rest of
// the setup domain does, and the content is never named on the command line.
func writeCommand(f File) string {
	tmp := connection.ShellArg(f.Path + ".staging")
	dest := connection.ShellArg(f.Path)
	return strings.Join([]string{
		fmt.Sprintf("sudo rm -f %s", tmp),
		fmt.Sprintf("sudo tee %s >/dev/null", tmp),
		fmt.Sprintf("sudo chmod %s %s", f.Mode, tmp),
		fmt.Sprintf("sudo chown %s %s", f.Owner, tmp),
		fmt.Sprintf("sudo mv -f %s %s", tmp, dest),
	}, " && ")
}
