package status

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
)

// scriptedConn answers one remote command with canned output and a canned
// error, and records what it was asked to run.
type scriptedConn struct {
	out  string
	err  error
	cmds []string
}

func (c *scriptedConn) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	c.cmds = append(c.cmds, cmd)
	if _, err := io.WriteString(stdout, c.out); err != nil {
		return err
	}
	return c.err
}

func TestReachableReportsAnAnsweringBox(t *testing.T) {
	got, err := Reachable(context.Background(), &scriptedConn{})
	if err != nil {
		t.Fatalf("Reachable() err = %v, want nil", err)
	}
	if !got {
		t.Error("Reachable() = false, want true for a box that answered")
	}
}

func TestReachableReportsAConnectFailureAsUnreachableNotAnError(t *testing.T) {
	got, err := Reachable(context.Background(), &scriptedConn{err: connection.ErrConnect})
	if err != nil {
		t.Fatalf("Reachable() err = %v, want a connect failure reported as unreachable", err)
	}
	if got {
		t.Error("Reachable() = true, want false for a box that refused the connection")
	}
}

func TestReachableSurfacesAnythingElseAsAnError(t *testing.T) {
	boom := errors.New("ssh binary is missing")

	got, err := Reachable(context.Background(), &scriptedConn{err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("Reachable() err = %v, want the underlying failure", err)
	}
	if got {
		t.Error("Reachable() = true, want false alongside an error")
	}
}

func TestReadMarkerDecodesTheBoxsMarker(t *testing.T) {
	conn := &scriptedConn{out: `{"schema_version":1,"access_mode":"public","blueprint":"acme"}`}

	m, present, err := ReadMarker(context.Background(), conn)
	if err != nil {
		t.Fatalf("ReadMarker() err = %v, want nil", err)
	}
	if !present {
		t.Fatal("ReadMarker() present = false, want true for a box carrying a marker")
	}
	if m.AccessMode != "public" || m.Blueprint != "acme" {
		t.Errorf("ReadMarker() marker = %+v, want the box's recorded facts", m)
	}
	if len(conn.cmds) != 1 || !strings.Contains(conn.cmds[0], marker.Path) {
		t.Errorf("commands run = %v, want one read of %s", conn.cmds, marker.Path)
	}
}

func TestReadMarkerReportsABoxCarryingNone(t *testing.T) {
	_, present, err := ReadMarker(context.Background(), &scriptedConn{out: "\n"})
	if err != nil {
		t.Fatalf("ReadMarker() err = %v, want an absent marker reported, not an error", err)
	}
	if present {
		t.Error("ReadMarker() present = true, want false for a box carrying no marker")
	}
}

func TestReadMarkerRefusesAMalformedMarker(t *testing.T) {
	_, _, err := ReadMarker(context.Background(), &scriptedConn{out: "{not json"})
	if err == nil {
		t.Fatal("ReadMarker() err = nil, want an error for a marker smith could not decode")
	}
	if !strings.Contains(err.Error(), marker.Path) {
		t.Errorf("ReadMarker() err = %q, want it to name %s", err, marker.Path)
	}
}
