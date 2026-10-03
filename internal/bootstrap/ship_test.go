package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// shipConn fakes the box at the Conn seam for shipping: mktemp hands out a
// fresh directory per call, and it records what was copied where, which
// commands ran, and whether the context was still live when each ran.
type shipConn struct {
	mktempErr error
	copyErr   error
	rmErr     error

	dirs     int
	copies   map[string]string // remote path -> script contents
	runs     []string
	rmCtxErr []error
}

func (c *shipConn) Copy(_ context.Context, localPath, remotePath string) error {
	if c.copyErr != nil {
		return c.copyErr
	}
	b, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read shipped file: %w", err)
	}
	if c.copies == nil {
		c.copies = map[string]string{}
	}
	c.copies[remotePath] = string(b)
	return nil
}

func (c *shipConn) Run(ctx context.Context, cmd string, stdout, _ io.Writer) error {
	c.runs = append(c.runs, cmd)
	switch {
	case strings.HasPrefix(cmd, "mktemp"):
		if c.mktempErr != nil {
			return c.mktempErr
		}
		c.dirs++
		_, err := fmt.Fprintln(stdout, shippedDir(c.dirs))
		return err
	case strings.HasPrefix(cmd, "rm "):
		c.rmCtxErr = append(c.rmCtxErr, ctx.Err())
		return c.rmErr
	}
	return nil
}

// removed reports whether an rm of dir ran against the box.
func (c *shipConn) removed(dir string) bool {
	for _, cmd := range c.runs {
		if strings.HasPrefix(cmd, "rm ") && strings.Contains(cmd, connection.ShellArg(dir)) {
			return true
		}
	}
	return false
}

func TestShipGivesEachShipItsOwnPath(t *testing.T) {
	conn := &shipConn{}
	first, err := Ship(context.Background(), conn, "echo one")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	second, err := Ship(context.Background(), conn, "echo two")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	if first.Path() == second.Path() {
		t.Errorf("two ships both returned %q, want different paths", first.Path())
	}
	for _, p := range []string{first.Path(), second.Path()} {
		if path.Dir(p) == "/tmp" {
			t.Errorf("shipped path %q is a fixed name under /tmp, want one inside a private directory", p)
		}
	}
}

func TestShipCopiesTheScriptIntoTheDirectoryMktempMade(t *testing.T) {
	conn := &shipConn{}
	s, err := Ship(context.Background(), conn, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	if got := path.Dir(s.Path()); got != shippedDir(1) {
		t.Errorf("shipped into %q, want the mktemp directory %q", got, shippedDir(1))
	}
	if got := conn.copies[s.Path()]; got != "echo hi" {
		t.Errorf("copied %q to %q, want the script", got, s.Path())
	}
}

func TestShipCopiesTheScriptIntoAPrivateDirectory(t *testing.T) {
	box := newLocalBox(t, t.TempDir())
	s, err := Ship(context.Background(), box, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	info, err := os.Stat(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatalf("stat shipped directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("shipped directory mode = %v, want %v", got, os.FileMode(0o700))
	}
	got, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read shipped script: %v", err)
	}
	if string(got) != "echo hi" {
		t.Errorf("shipped script = %q, want %q", got, "echo hi")
	}
}

func TestShipMakesADistinctDirectoryUnderTheBoxTMPDIR(t *testing.T) {
	tmp := t.TempDir()
	box := newLocalBox(t, tmp)
	dirs := map[string]bool{}
	for _, script := range []string{"echo one", "echo two"} {
		s, err := Ship(context.Background(), box, script)
		if err != nil {
			t.Fatalf("Ship() error = %v", err)
		}
		dir := filepath.Dir(s.Path())
		if filepath.Dir(dir) != tmp {
			t.Errorf("shipped into %q, want a directory of its own under TMPDIR %q", dir, tmp)
		}
		dirs[dir] = true
	}
	if len(dirs) != 2 {
		t.Errorf("two ships used directories %v, want two distinct ones", dirs)
	}
}

// localBox is a box at the Conn seam backed by the host's real filesystem: Run
// runs each command under sh with the box's TMPDIR, and Copy writes the file to
// the remote path as scp would. It tests what a shipped directory is, not how
// the command that made it is spelled.
type localBox struct {
	env []string
}

// newLocalBox returns a localBox whose TMPDIR is tmpdir. The box runs Ubuntu,
// so its mktemp is GNU coreutils; the host's mktemp is used only when it is
// GNU too (gmktemp on macOS), and the test skips, saying why, when neither is.
func newLocalBox(t *testing.T, tmpdir string) localBox {
	t.Helper()
	mktemp := gnuMktemp()
	if mktemp == "" {
		t.Skip("no GNU coreutils mktemp on this host to stand in for the box's (on macOS: brew install coreutils)")
	}
	bin := t.TempDir()
	if err := os.Symlink(mktemp, filepath.Join(bin, "mktemp")); err != nil {
		t.Fatalf("link GNU mktemp: %v", err)
	}
	return localBox{env: []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + tmpdir,
	}}
}

// gnuMktemp is the path of a GNU coreutils mktemp on the host, or "" if there
// is none.
func gnuMktemp() string {
	for _, name := range []string{"mktemp", "gmktemp"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(p, "--version").Output()
		if err == nil && strings.Contains(string(out), "GNU coreutils") {
			return p
		}
	}
	return ""
}

func (b localBox) Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env = b.env
	c.Stdout = stdout
	c.Stderr = stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("run %q: %w", cmd, err)
	}
	return nil
}

func (b localBox) Copy(_ context.Context, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read shipped file: %w", err)
	}
	if err := os.WriteFile(remotePath, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", remotePath, err)
	}
	return nil
}

func TestShippedRemoveDeletesTheDirectory(t *testing.T) {
	conn := &shipConn{}
	s, err := Ship(context.Background(), conn, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	s.Remove(context.Background())
	if !conn.removed(path.Dir(s.Path())) {
		t.Errorf("runs = %q, want the shipped directory removed", conn.runs)
	}
}

func TestShippedRemoveStillRunsUnderACancelledContext(t *testing.T) {
	conn := &shipConn{}
	s, err := Ship(context.Background(), conn, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Remove(ctx)
	if len(conn.rmCtxErr) != 1 || conn.rmCtxErr[0] != nil {
		t.Errorf("remove ran with context errors %v, want one run on a live context", conn.rmCtxErr)
	}
}

func TestShippedRemoveSwallowsAFailure(t *testing.T) {
	conn := &shipConn{rmErr: errors.New("rm: permission denied")}
	s, err := Ship(context.Background(), conn, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	s.Remove(context.Background()) // must neither panic nor report
	if !conn.removed(path.Dir(s.Path())) {
		t.Errorf("runs = %q, want the remove attempted", conn.runs)
	}
}

func TestShipFailedCopyRemovesTheDirectoryItMade(t *testing.T) {
	conn := &shipConn{copyErr: errors.New("scp: exit status 1")}
	if _, err := Ship(context.Background(), conn, "echo hi"); err == nil {
		t.Fatal("Ship() error = nil, want the copy failure")
	}
	if !conn.removed(shippedDir(1)) {
		t.Errorf("runs = %q, want the directory mktemp made removed", conn.runs)
	}
}

func TestShipKeepsAConnectFailureClassified(t *testing.T) {
	tests := []struct {
		name string
		conn *shipConn
	}{
		{"mktemp", &shipConn{mktempErr: connection.ErrConnect}},
		{"copy", &shipConn{copyErr: connection.ErrConnect}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Ship(context.Background(), tt.conn, "echo hi")
			if !errors.Is(err, connection.ErrConnect) {
				t.Errorf("Ship() error = %v, want it to wrap ErrConnect", err)
			}
		})
	}
}

func TestShipFailedMktempRemovesNothing(t *testing.T) {
	conn := &shipConn{mktempErr: errors.New("mktemp: failed")}
	if _, err := Ship(context.Background(), conn, "echo hi"); err == nil {
		t.Fatal("Ship() error = nil, want the mktemp failure")
	}
	for _, cmd := range conn.runs {
		if strings.HasPrefix(cmd, "rm ") {
			t.Errorf("ran %q, want nothing removed when no directory was made", cmd)
		}
	}
}

func TestShippedReachedOverAnotherConnectionIsRemovedThere(t *testing.T) {
	public, tailnet := &shipConn{}, &shipConn{}
	s, err := Ship(context.Background(), public, "echo hi")
	if err != nil {
		t.Fatalf("Ship() error = %v", err)
	}

	moved := s.Over(tailnet)
	moved.Remove(context.Background())

	if moved.Path() != s.Path() {
		t.Errorf("Over() path = %q, want the same shipped script %q", moved.Path(), s.Path())
	}
	if !tailnet.removed(shippedDir(1)) {
		t.Errorf("tailnet ran %q, want the shipped directory removed over it", tailnet.runs)
	}
	if public.removed(shippedDir(1)) {
		t.Error("the shipped directory was removed over the connection it was shipped over, want only the one it was moved to")
	}
}
