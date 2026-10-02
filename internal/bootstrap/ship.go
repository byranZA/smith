package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

// removeTimeout bounds how long Remove waits on the box, so an unresponsive box
// cannot hang a command's exit.
const removeTimeout = 5 * time.Second

// shippedName is the file name a shipped script takes inside its private
// directory.
const shippedName = "script.sh"

// Shipped is a shipped script: one script copied into a private directory on
// the box, owned by the command and login that shipped it. Every call a command
// makes over the same connection runs Path; Remove deletes the directory when
// the command ends.
type Shipped struct {
	conn Conn
	dir  string
}

// Path is the raw remote path of the shipped script. mktemp honours the box's
// TMPDIR, so the path may hold whitespace or shell metacharacters: a caller
// interpolating it into a remote command quotes it with connection.ShellArg,
// as in `bash <quoted path> <subcommand>`.
func (s Shipped) Path() string {
	return s.dir + "/" + shippedName
}

// Remove deletes the shipped script's directory from the box, best effort. It
// runs even when ctx is already cancelled, so an interrupted command still
// cleans up, but waits on the box for a few seconds at most. A failed remove is
// never reported: it must not change the command's result, and the directory is
// private to this command, so nothing later trips over it.
func (s Shipped) Remove(ctx context.Context) {
	if s.dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	// Best effort by design (see above), so the error is deliberately dropped.
	_ = s.conn.Run(ctx, "rm -rf -- "+connection.ShellArg(s.dir), io.Discard, io.Discard)
}

// Ship copies script to the box over conn as a shipped script: it makes a fresh
// private directory with mktemp, which honours the box's TMPDIR, and copies the
// script into it. No two ships share a path, so a file another command or login
// left behind can never block this one. A copy that fails removes the directory
// before returning; a connect failure stays wrapped as connection.ErrConnect.
func Ship(ctx context.Context, conn Conn, script string) (Shipped, error) {
	var out bytes.Buffer
	if err := conn.Run(ctx, "mktemp -d -t smith.XXXXXXXX", &out, io.Discard); err != nil {
		return Shipped{}, fmt.Errorf("make shipped script directory: %w", err)
	}
	dir := strings.TrimSpace(out.String())
	if dir == "" {
		return Shipped{}, errors.New("make shipped script directory: mktemp printed no path")
	}
	s := Shipped{conn: conn, dir: dir}
	if err := ShipTo(ctx, conn, script, s.Path()); err != nil {
		s.Remove(ctx)
		return Shipped{}, err
	}
	return s, nil
}

// ShipTo writes a shell artifact to a local temp file and copies it to
// remotePath on the box over conn. It does no more than copy: production code
// ships through Ship, which gives every script a private path. ShipTo stays
// exported so tests can place a file at a chosen path, such as a stale script
// an earlier release left behind.
func ShipTo(ctx context.Context, conn Conn, script, remotePath string) error {
	f, err := os.CreateTemp("", "smith-script-*.sh")
	if err != nil {
		return fmt.Errorf("create temp script: %w", err)
	}
	// Best-effort cleanup of the OS temp file: a failed remove is unrecoverable
	// here and harmless (the OS reclaims its temp dir), so the error is
	// deliberately not propagated.
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString(script); err != nil {
		writeErr := fmt.Errorf("write temp script: %w", err)
		// The write already failed; close best-effort and join any close error
		// so a failed close can't silently mask the underlying write failure.
		if cerr := f.Close(); cerr != nil {
			return errors.Join(writeErr, fmt.Errorf("close temp script: %w", cerr))
		}
		return writeErr
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp script: %w", err)
	}
	if err := conn.Copy(ctx, f.Name(), remotePath); err != nil {
		return fmt.Errorf("copy script to box: %w", err)
	}
	return nil
}
