package boxname

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
)

// fakeBox answers marker reads with its marker and replaces it on a tee write.
type fakeBox struct {
	marker      string
	unreachable bool
}

func (b *fakeBox) Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	return b.RunWithInput(ctx, cmd, strings.NewReader(""), stdout, stderr)
}

func (b *fakeBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, stdout, _ io.Writer) error {
	if b.unreachable {
		return fmt.Errorf("ssh smith@100.92.14.7: %w", connection.ErrUnreachable)
	}
	if strings.Contains(cmd, "tee") {
		data, err := io.ReadAll(stdin)
		b.marker = string(data)
		return err
	}
	if strings.Contains(cmd, "cat") {
		_, err := io.WriteString(stdout, b.marker)
		return err
	}
	return nil
}

const namedA = `{
  "schema_version": 2,
  "access_mode": "public",
  "name": "a",
  "completed_phases": ["packages"]
}
`

const namedB = `{
  "schema_version": 2,
  "access_mode": "public",
  "name": "b",
  "completed_phases": ["packages"]
}
`

// aToB renames box "a" at smith@100.92.14.7 to "b".
var aToB = Change{From: "a", To: "b", Target: "smith@100.92.14.7"}

// registry holds an inventory with box "a" and registers renames into it.
type registry struct{ inv inventory.Inventory }

func newRegistry() *registry {
	return &registry{inv: inventory.Inventory{SchemaVersion: inventory.SchemaVersion, Boxes: map[string]inventory.Box{
		"a": {Target: "smith@100.92.14.7"},
	}}}
}

func (r *registry) register(c Change) error {
	next, err := inventory.Rename(r.inv, c.From, c.To, c.Target)
	r.inv = next
	return err
}

func TestRenameRewritesTheBoxsMarker(t *testing.T) {
	t.Parallel()
	box := &fakeBox{marker: namedA}

	if err := Rename(t.Context(), box, aToB, newRegistry().register); err != nil {
		t.Fatalf("Rename(a, b) err = %v, want nil", err)
	}
	if box.marker != namedB {
		t.Errorf("Rename(a, b) marker = %q, want %q", box.marker, namedB)
	}
}

func TestRenameMovesTheRegistration(t *testing.T) {
	t.Parallel()
	reg := newRegistry()

	if err := Rename(t.Context(), &fakeBox{marker: namedA}, aToB, reg.register); err != nil {
		t.Fatalf("Rename(a, b) err = %v, want nil", err)
	}
	want := map[string]inventory.Box{"b": {Target: "smith@100.92.14.7"}}
	if !reflect.DeepEqual(reg.inv.Boxes, want) {
		t.Errorf("Rename(a, b) inventory = %v, want %v", reg.inv.Boxes, want)
	}
}

func TestRenameRefusesAnUnreachableBoxAndRegistersNothing(t *testing.T) {
	t.Parallel()
	reg := newRegistry()

	err := Rename(t.Context(), &fakeBox{marker: namedA, unreachable: true}, aToB, reg.register)

	var unreachable *UnreachableError
	if !errors.As(err, &unreachable) || !errors.Is(err, connection.ErrConnect) {
		t.Fatalf("Rename(a, b) err = %v, want an UnreachableError wrapping the connect failure", err)
	}
	if _, ok := reg.inv.Boxes["a"]; !ok {
		t.Errorf("Rename(a, b) inventory = %v, want a still registered", reg.inv.Boxes)
	}
}

func TestRenameRefusesABoxCarryingNoMarkerAndRegistersNothing(t *testing.T) {
	t.Parallel()
	reg := newRegistry()

	err := Rename(t.Context(), &fakeBox{}, aToB, reg.register)

	if !errors.Is(err, ErrNoMarker) {
		t.Fatalf("Rename(a, b) err = %v, want ErrNoMarker", err)
	}
	if _, ok := reg.inv.Boxes["a"]; !ok {
		t.Errorf("Rename(a, b) inventory = %v, want a still registered", reg.inv.Boxes)
	}
}

func TestRenameReportsAMarkerRenamedThatTheInventoryDidNotFollow(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk full")
	failing := func(Change) error { return cause }

	err := Rename(t.Context(), &fakeBox{marker: namedA}, aToB, failing)

	var partial *PartialError
	if !errors.As(err, &partial) || partial.Change != aToB || !errors.Is(err, cause) {
		t.Errorf("Rename(a, b) err = %v, want a PartialError for a to b wrapping %v", err, cause)
	}
}
