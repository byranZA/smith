package inventory

import (
	"errors"
	"strings"
	"testing"
)

func TestRenameMovesTheEntryToTheNewName(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@203.0.113.10"},
	}}

	next, err := Rename(inv, "dev", "staging", "smith@203.0.113.10")
	if err != nil {
		t.Fatalf("Rename() err = %v, want nil", err)
	}
	if got := next.Boxes["staging"].Target; got != "smith@203.0.113.10" {
		t.Errorf("Rename() target for staging = %q, want %q", got, "smith@203.0.113.10")
	}
	if _, ok := next.Boxes["dev"]; ok {
		t.Errorf("Rename() left %q registered, want the entry moved rather than copied", "dev")
	}
}

func TestRenameStoresTheTargetItWasGivenRatherThanTheOldOne(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@100.92.14.7"},
	}}

	next, err := Rename(inv, "dev", "dev", "smith@100.92.14.31")
	if err != nil {
		t.Fatalf("Rename() err = %v, want nil", err)
	}
	if got := next.Boxes["dev"].Target; got != "smith@100.92.14.31" {
		t.Errorf("Rename() target for dev = %q, want the address smith just proved", got)
	}
	if len(next.Boxes) != 1 {
		t.Errorf("Rename() boxes = %v, want exactly one entry", next.Boxes)
	}
}

func TestRenameRegistersABoxThatWasNotRegisteredBefore(t *testing.T) {
	next, err := Rename(Empty(), "", "dev", "smith@203.0.113.10")
	if err != nil {
		t.Fatalf("Rename() err = %v, want nil", err)
	}
	if got := next.Boxes["dev"].Target; got != "smith@203.0.113.10" {
		t.Errorf("Rename() target for dev = %q, want the box registered under its new name", got)
	}
}

func TestRenameRefusesANameHeldByADifferentBox(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev":     {Target: "smith@203.0.113.10"},
		"staging": {Target: "smith@198.51.100.7"},
	}}

	_, err := Rename(inv, "dev", "staging", "smith@203.0.113.10")

	var collision *NameCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("Rename() err = %v, want a NameCollisionError", err)
	}
	if collision.Name != "staging" || collision.Registered != "smith@198.51.100.7" {
		t.Errorf("NameCollisionError = %+v, want it to carry both sides of the collision", collision)
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("Rename() err = %q, want it to name the flag to pass instead", err)
	}
	if got := inv.Boxes["staging"].Target; got != "smith@198.51.100.7" {
		t.Errorf("staging = %q, want a refused rename to leave the inventory unchanged", got)
	}
	if _, ok := inv.Boxes["dev"]; !ok {
		t.Errorf("dev is gone, want a refused rename to leave the inventory unchanged")
	}
}

func TestRenameDoesNotMutateTheInventoryItWasGiven(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@203.0.113.10"},
	}}

	if _, err := Rename(inv, "dev", "staging", "smith@203.0.113.10"); err != nil {
		t.Fatalf("Rename() err = %v, want nil", err)
	}
	if _, ok := inv.Boxes["dev"]; !ok {
		t.Errorf("Rename() removed %q from the inventory it was given, want it returned as a new value", "dev")
	}
	if _, ok := inv.Boxes["staging"]; ok {
		t.Errorf("Rename() added %q to the inventory it was given, want it returned as a new value", "staging")
	}
}

func TestRenameLeavesASecondNameForOneBoxAlone(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev":  {Target: "smith@203.0.113.10"},
		"work": {Target: "smith@203.0.113.10"},
	}}

	next, err := Rename(inv, "dev", "staging", "smith@203.0.113.10")
	if err != nil {
		t.Fatalf("Rename() err = %v, want nil", err)
	}
	if got := next.Boxes["work"].Target; got != "smith@203.0.113.10" {
		t.Errorf("work = %q, want two names for one box to stay legal", got)
	}
}

func TestRenameOntoASecondNameForTheSameBoxUpdatesItRatherThanRefusing(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev":  {Target: "smith@100.92.14.7"},
		"work": {Target: "smith@100.92.14.7"},
	}}

	next, err := Rename(inv, "dev", "work", "smith@100.92.14.31")
	if err != nil {
		t.Fatalf("Rename() err = %v, want a name already reaching this very box not to collide", err)
	}
	if got := next.Boxes["work"].Target; got != "smith@100.92.14.31" {
		t.Errorf("work = %q, want the address smith just proved", got)
	}
	if _, ok := next.Boxes["dev"]; ok {
		t.Errorf("dev is still registered, want the entry moved")
	}
}

func TestPreviousNameIsTheMarkersNameWhenItsEntryReachesTheAddressTheRunCameInOver(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@100.92.14.7"},
	}}

	if got := PreviousName(inv, "dev", "smith@100.92.14.7", "smith@100.92.14.31"); got != "dev" {
		t.Errorf("PreviousName() = %q, want %q: the entry is this box's, at the address it answered at before", got, "dev")
	}
}

func TestPreviousNameIsTheMarkersNameWhenItsEntryAlreadyReachesTheTargetBeingRegistered(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@203.0.113.10"},
	}}

	if got := PreviousName(inv, "dev", "root@203.0.113.10", "smith@203.0.113.10"); got != "dev" {
		t.Errorf("PreviousName() = %q, want %q: the entry already reaches this very box", got, "dev")
	}
}

func TestPreviousNameIsEmptyWhenTheMarkersNameReachesADifferentBox(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@203.0.113.10"},
	}}

	if got := PreviousName(inv, "dev", "root@198.51.100.7", "smith@198.51.100.7"); got != "" {
		t.Errorf("PreviousName() = %q, want no entry to move: %q names another machine", got, "dev")
	}
}

func TestPreviousNameIsEmptyWhenNothingIsRegisteredUnderTheMarkersName(t *testing.T) {
	if got := PreviousName(Empty(), "dev", "root@203.0.113.10", "smith@203.0.113.10"); got != "" {
		t.Errorf("PreviousName() = %q, want no entry to move when the name is unregistered", got)
	}
}

func TestPreviousNameIsEmptyWhenTheBoxRecordsNoName(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev": {Target: "smith@203.0.113.10"},
	}}

	if got := PreviousName(inv, "", "smith@203.0.113.10", "smith@203.0.113.10"); got != "" {
		t.Errorf("PreviousName() = %q, want no entry to move when the box records no name", got)
	}
}
