package connection

import (
	"context"
	"errors"
	"io"
	"testing"
)

type probedConn struct {
	err error
}

func (c *probedConn) Run(_ context.Context, _ string, _, _ io.Writer) error {
	return c.err
}

func TestReachableReportsAnAnsweringBox(t *testing.T) {
	t.Parallel()
	got, err := Reachable(context.Background(), &probedConn{})
	if err != nil {
		t.Fatalf("Reachable() err = %v, want nil", err)
	}
	if !got {
		t.Error("Reachable() = false, want true for a box that answered")
	}
}

func TestReachableReportsAConnectFailureAsUnreachableNotAnError(t *testing.T) {
	t.Parallel()
	got, err := Reachable(context.Background(), &probedConn{err: ErrConnect})
	if err != nil {
		t.Fatalf("Reachable() err = %v, want a connect failure reported as unreachable", err)
	}
	if got {
		t.Error("Reachable() = true, want false for a box that refused the connection")
	}
}

func TestReachableSurfacesAnythingElseAsAnError(t *testing.T) {
	t.Parallel()
	boom := errors.New("ssh binary is missing")

	got, err := Reachable(context.Background(), &probedConn{err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("Reachable() err = %v, want the underlying failure", err)
	}
	if got {
		t.Error("Reachable() = true, want false alongside an error")
	}
}
