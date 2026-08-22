package connection

import (
	"context"
	"errors"
	"io"
	"testing"
)

// probedConn answers a remote command with a canned error and records what it
// was asked to run.
type probedConn struct {
	err  error
	cmds []string
}

func (c *probedConn) Run(_ context.Context, cmd string, _, _ io.Writer) error {
	c.cmds = append(c.cmds, cmd)
	return c.err
}

func TestReachableReportsAnAnsweringBox(t *testing.T) {
	conn := &probedConn{}

	got, err := Reachable(context.Background(), conn)
	if err != nil {
		t.Fatalf("Reachable() err = %v, want nil", err)
	}
	if !got {
		t.Error("Reachable() = false, want true for a box that answered")
	}
	if len(conn.cmds) != 1 {
		t.Errorf("commands run = %v, want one probe", conn.cmds)
	}
}

func TestReachableReportsAConnectFailureAsUnreachableNotAnError(t *testing.T) {
	got, err := Reachable(context.Background(), &probedConn{err: ErrConnect})
	if err != nil {
		t.Fatalf("Reachable() err = %v, want a connect failure reported as unreachable", err)
	}
	if got {
		t.Error("Reachable() = true, want false for a box that refused the connection")
	}
}

func TestReachableSurfacesAnythingElseAsAnError(t *testing.T) {
	boom := errors.New("ssh binary is missing")

	got, err := Reachable(context.Background(), &probedConn{err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("Reachable() err = %v, want the underlying failure", err)
	}
	if got {
		t.Error("Reachable() = true, want false alongside an error")
	}
}
