package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
	// Staged means the file was written: the box either held nothing there or
	// held different bytes.
	Staged
)

// String renders the change as the operator is told it.
func (c Change) String() string {
	if c == Staged {
		return "staged"
	}
	return "unchanged"
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
// The bytes travel over stdin and never appear in an argument, and each write
// lands on a temporary path beside its destination before it is chmod'ed,
// chown'ed and moved into place, so the destination never exists holding
// partial content or a wider mode.
func Converge(ctx context.Context, conn Conn, tree Tree) (Result, error) {
	change, err := write(ctx, conn, tree.Document)
	if err != nil {
		return Result{}, err
	}
	return Result{Entries: []Entry{{Path: tree.Document.Path, Change: change}}}, nil
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
	return Staged, nil
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

// writeCommand builds the remote write: ensure the config directory, take the
// bytes from stdin into a temporary path beside the destination, give the
// temporary file the mode and owner the destination is to carry, and move it
// into place. Every step runs through sudo, as the rest of the setup domain
// does, and the content is never named on the command line.
func writeCommand(f File) string {
	tmp := connection.ShellArg(f.Path + ".staging")
	dest := connection.ShellArg(f.Path)
	return strings.Join([]string{
		fmt.Sprintf("sudo install -d -m 0755 -o root -g root %s", connection.ShellArg(Root)),
		fmt.Sprintf("sudo rm -f %s", tmp),
		fmt.Sprintf("sudo tee %s >/dev/null", tmp),
		fmt.Sprintf("sudo chmod %s %s", f.Mode, tmp),
		fmt.Sprintf("sudo chown %s %s", f.Owner, tmp),
		fmt.Sprintf("sudo mv -f %s %s", tmp, dest),
	}, " && ")
}
