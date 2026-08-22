package status

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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

// blockingConn answers only once every other blockingConn has been asked, so a
// serial fan-out never completes.
type blockingConn struct {
	arrived *sync.WaitGroup
	err     error
}

// Run signals its arrival and waits for the rest before answering.
func (c blockingConn) Run(_ context.Context, _ string, _, _ io.Writer) error {
	c.arrived.Done()
	c.arrived.Wait()
	return c.err
}

func TestProbeAllReportsEachBoxsReachability(t *testing.T) {
	conns := map[string]Conn{
		"dev":     &scriptedConn{},
		"scratch": &scriptedConn{err: connection.ErrConnect},
	}

	reach, err := ProbeAll(context.Background(), conns)
	if err != nil {
		t.Fatalf("ProbeAll() err = %v, want nil", err)
	}
	if want := map[string]bool{"dev": true, "scratch": false}; !reflect.DeepEqual(reach, want) {
		t.Errorf("ProbeAll() = %v, want %v", reach, want)
	}
}

func TestProbeAllProbesBoxesConcurrently(t *testing.T) {
	var arrived sync.WaitGroup
	conns := map[string]Conn{}
	for _, name := range []string{"api", "dev", "scratch"} {
		arrived.Add(1)
		conns[name] = blockingConn{arrived: &arrived}
	}

	done := make(chan error, 1)
	go func() {
		_, err := ProbeAll(context.Background(), conns)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProbeAll() err = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ProbeAll() did not finish, want every box probed concurrently")
	}
}

func TestProbeAllSurfacesAProbeThatCouldNotRun(t *testing.T) {
	boom := errors.New("ssh binary is missing")
	conns := map[string]Conn{"dev": &scriptedConn{}, "scratch": &scriptedConn{err: boom}}

	reach, err := ProbeAll(context.Background(), conns)
	if !errors.Is(err, boom) {
		t.Fatalf("ProbeAll() err = %v, want the underlying failure", err)
	}
	if reach != nil {
		t.Errorf("ProbeAll() = %v, want no results alongside an error", reach)
	}
}

func TestProbeAllOnNoBoxesProbesNothing(t *testing.T) {
	reach, err := ProbeAll(context.Background(), map[string]Conn{})
	if err != nil {
		t.Fatalf("ProbeAll() err = %v, want nil", err)
	}
	if len(reach) != 0 {
		t.Errorf("ProbeAll() = %v, want no results", reach)
	}
}
