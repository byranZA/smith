package inventory

import (
	"errors"
	"strings"
	"testing"
)

func TestForgetRemovesTheEntry(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev":     {Target: "smith@100.92.14.7"},
		"scratch": {Target: "smith@203.0.113.42"},
	}}

	next, err := Forget(inv, "dev")
	if err != nil {
		t.Fatalf("Forget() err = %v, want nil", err)
	}
	if _, ok := next.Boxes["dev"]; ok {
		t.Errorf("Forget() left %q registered, want it removed", "dev")
	}
	if got := next.Boxes["scratch"].Target; got != "smith@203.0.113.42" {
		t.Errorf("Forget() target for scratch = %q, want the other box untouched", got)
	}
}

func TestForgetDoesNotMutateTheInventoryItWasGiven(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@100.92.14.7"},
	}}

	if _, err := Forget(inv, "dev"); err != nil {
		t.Fatalf("Forget() err = %v, want nil", err)
	}
	if _, ok := inv.Boxes["dev"]; !ok {
		t.Errorf("Forget() removed %q from the inventory it was given, want it returned as a new value", "dev")
	}
}

func TestForgetRefusesANameThatIsNotRegistered(t *testing.T) {
	inv := Empty()

	_, err := Forget(inv, "staging")

	var unknown *UnknownBoxError
	if !errors.As(err, &unknown) {
		t.Fatalf("Forget() err = %v, want an UnknownBoxError", err)
	}
	if unknown.Name != "staging" {
		t.Errorf("UnknownBoxError.Name = %q, want %q", unknown.Name, "staging")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("Forget() err = %q, want it to name the box that is not registered", err)
	}
}

func TestForgetKeepsTheSchemaVersionItWasGiven(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@100.92.14.7"},
	}}

	next, err := Forget(inv, "dev")
	if err != nil {
		t.Fatalf("Forget() err = %v, want nil", err)
	}
	if next.SchemaVersion != inv.SchemaVersion {
		t.Errorf("Forget() schema version = %d, want %d", next.SchemaVersion, inv.SchemaVersion)
	}
}
