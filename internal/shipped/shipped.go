// Package shipped owns the life of a shipped script: a script smith copies onto
// the box into a private directory on first use, drives by subcommand, and
// removes best effort when the stage that shipped it is done with it.
package shipped

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

// ErrNotShipped marks a script that could not be shipped for a reason other
// than connecting: nothing ran on the box. Callers branch on it with errors.Is.
var ErrNotShipped = errors.New("nothing ran on the box")

// removeTimeout bounds how long Close waits on the box, so an unresponsive box
// cannot hang a command's exit.
const removeTimeout = 5 * time.Second

// shippedName is the file name the script takes inside its private directory.
const shippedName = "script.sh"

// Conn is the slice of a connection a shipped script needs: copy a file to the
// box and run a remote command with streamed output, optionally fed stdin.
type Conn interface {
	Copy(ctx context.Context, localPath, remotePath string) error
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
	RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Script is one use of a shipped script over a connection. It ships on the
// first Run and is removed by Close; it is not safe for concurrent use.
type Script struct {
	conn  Conn
	name  string
	body  string
	dir   string
	spent bool
}

// New returns a Script that ships body, named name (such as "bootstrap.sh"),
// over conn when it first runs. Nothing reaches the box until then.
func New(conn Conn, name, body string) *Script {
	return &Script{conn: conn, name: name, body: body}
}

// Run runs subcommand sub of the script with args on the box, shipping the
// script first if this is the first run, and streams its output to stdout and
// stderr. A connect failure wraps connection.ErrConnect; any other failure to
// ship wraps ErrNotShipped; a subcommand that ran and failed wraps neither.
func (s *Script) Run(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error {
	if err := s.ship(ctx); err != nil {
		return err
	}
	return s.run(ctx, s.dir, stdout, stderr, sub, args)
}

// RunFinal is Run for a subcommand that removes the script's private directory
// itself: it appends "--remove-dir <dir>" to args and leaves the handle spent
// whatever the outcome, so Close does nothing and any later run is refused.
func (s *Script) RunFinal(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error {
	err := s.ship(ctx)
	dir := s.dir
	s.dir, s.spent = "", true
	if err != nil {
		return err
	}
	return s.run(ctx, dir, stdout, stderr, sub, slices.Concat(args, []string{"--remove-dir", dir}))
}

// RunWithInput is Run with stdin fed to the subcommand, so a secret such as an
// auth key reaches the script without ever appearing in the box's argv.
func (s *Script) RunWithInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, sub string, args ...string) error {
	if err := s.ship(ctx); err != nil {
		return err
	}
	if err := s.conn.RunWithInput(ctx, command(s.dir, sub, args), stdin, stdout, stderr); err != nil {
		return fmt.Errorf("run %s %s: %w", s.name, sub, err)
	}
	return nil
}

// Move sends every later run, and Close, over conn — the box at a new address
// — reusing any copy already shipped. Dialing conn is the caller's job.
func (s *Script) Move(conn Conn) {
	s.conn = conn
}

// Close removes the script's private directory from the box, best effort. It
// runs even when ctx is cancelled, waits on the box a few seconds at most,
// never reports a failure, and does nothing when nothing is shipped or a final
// subcommand has run.
func (s *Script) Close(ctx context.Context) {
	if s.dir == "" {
		return
	}
	dir := s.dir
	s.dir = ""
	removeDir(ctx, s.conn, dir)
}

// run runs subcommand sub with args from the copy shipped to dir and streams
// its output, wrapping a failure with the script and subcommand it ran.
func (s *Script) run(ctx context.Context, dir string, stdout, stderr io.Writer, sub string, args []string) error {
	if err := s.conn.Run(ctx, command(dir, sub, args), stdout, stderr); err != nil {
		return fmt.Errorf("run %s %s: %w", s.name, sub, err)
	}
	return nil
}

// command renders the remote invocation of subcommand sub with args from the
// copy shipped to dir, quoting the script's path and every argument.
func command(dir, sub string, args []string) string {
	words := []string{"bash", connection.ShellArg(dir + "/" + shippedName), connection.ShellArg(sub)}
	for _, a := range args {
		words = append(words, connection.ShellArg(a))
	}
	return strings.Join(words, " ")
}

// ship copies the script into a fresh private directory on the box unless it
// is already there. A copy that fails removes the directory it made.
func (s *Script) ship(ctx context.Context) error {
	if s.spent {
		return fmt.Errorf("run %s: its final subcommand has already run", s.name)
	}
	if s.dir != "" {
		return nil
	}
	dir, err := makeDir(ctx, s.conn)
	if err != nil {
		return s.shipError("make private directory", err)
	}
	if err := copyScript(ctx, s.conn, s.body, dir+"/"+shippedName); err != nil {
		removeDir(ctx, s.conn, dir)
		return s.shipError("copy", err)
	}
	s.dir = dir
	return nil
}

// shipError wraps a failed ship step, naming the script, as a connect failure
// when it is one and as ErrNotShipped otherwise.
func (s *Script) shipError(step string, err error) error {
	if errors.Is(err, connection.ErrConnect) {
		return fmt.Errorf("ship %s: %s: %w", s.name, step, err)
	}
	return fmt.Errorf("ship %s: %w: %s: %w", s.name, ErrNotShipped, step, err)
}

// makeDir makes a fresh private directory on the box with mktemp, which
// honours the box's TMPDIR. A failure carries the box's own error text.
func makeDir(ctx context.Context, conn Conn) (string, error) {
	var out, errOut bytes.Buffer
	if err := conn.Run(ctx, "mktemp -d -t smith.XXXXXXXX", &out, &errOut); err != nil {
		if text := strings.TrimSpace(errOut.String()); text != "" {
			return "", fmt.Errorf("%w: %s", err, text)
		}
		return "", fmt.Errorf("mktemp: %w", err)
	}
	dir := strings.TrimSpace(out.String())
	if dir == "" {
		return "", errors.New("mktemp printed no path")
	}
	return dir, nil
}

// removeDir deletes dir from the box, best effort and bounded by
// removeTimeout even when ctx is already cancelled.
func removeDir(ctx context.Context, conn Conn, dir string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	//nolint:errcheck // removal is best effort: it must never change the command's result
	conn.Run(ctx, "rm -rf -- "+connection.ShellArg(dir), io.Discard, io.Discard)
}

// copyScript writes body to a local temp file and copies it to remotePath on
// the box.
func copyScript(ctx context.Context, conn Conn, body, remotePath string) error {
	f, err := os.CreateTemp("", "smith-script-*.sh")
	if err != nil {
		return fmt.Errorf("create temp script: %w", err)
	}
	//nolint:errcheck // the local temp file is harmless if left; the OS reclaims its temp dir
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		return errors.Join(fmt.Errorf("write temp script: %w", err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp script: %w", err)
	}
	if err := conn.Copy(ctx, f.Name(), remotePath); err != nil {
		return fmt.Errorf("copy script to box: %w", err)
	}
	return nil
}
