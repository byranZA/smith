package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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
		_, err := fmt.Fprintf(stdout, "/tmp/smith.%08d\n", c.dirs)
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
	if got := path.Dir(s.Path()); got != "/tmp/smith.00000001" {
		t.Errorf("shipped into %q, want the mktemp directory /tmp/smith.00000001", got)
	}
	if got := conn.copies[s.Path()]; got != "echo hi" {
		t.Errorf("copied %q to %q, want the script", got, s.Path())
	}
}

func TestShipMakesAPrivateDirectoryWithMktemp(t *testing.T) {
	conn := &shipConn{}
	if _, err := Ship(context.Background(), conn, "echo hi"); err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	if len(conn.runs) == 0 || conn.runs[0] != "mktemp -d -t smith.XXXXXXXX" {
		t.Errorf("runs = %q, want the directory made with mktemp -d -t smith.XXXXXXXX", conn.runs)
	}
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
	if !conn.removed("/tmp/smith.00000001") {
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
