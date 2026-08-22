package inventory

import (
	"errors"
	"strings"
	"testing"
)

func TestRegisterAddsABoxUnderItsName(t *testing.T) {
	got, err := Register(Empty(), "dev", "smith@100.92.14.7")
	if err != nil {
		t.Fatalf("Register() err = %v, want nil", err)
	}
	if got.Boxes["dev"].Target != "smith@100.92.14.7" {
		t.Errorf("Register() boxes = %v, want dev at smith@100.92.14.7", got.Boxes)
	}
}

func TestRegisterDoesNotMutateTheInventoryItWasGiven(t *testing.T) {
	inv := Empty()

	if _, err := Register(inv, "dev", "smith@100.92.14.7"); err != nil {
		t.Fatalf("Register() err = %v, want nil", err)
	}
	if len(inv.Boxes) != 0 {
		t.Errorf("original boxes = %v, want it untouched", inv.Boxes)
	}
}

func TestRegisterIsANoOpForABoxAlreadyRegisteredAtTheSameTarget(t *testing.T) {
	inv := Empty()
	inv.Boxes["dev"] = Box{Target: "smith@100.92.14.7"}

	got, err := Register(inv, "dev", "smith@100.92.14.7")
	if err != nil {
		t.Fatalf("Register() err = %v, want re-registering the same target to succeed", err)
	}
	if len(got.Boxes) != 1 || got.Boxes["dev"].Target != "smith@100.92.14.7" {
		t.Errorf("Register() boxes = %v, want exactly the entry that was already there", got.Boxes)
	}
}

func TestRegisterRefusesANameHeldByADifferentBox(t *testing.T) {
	inv := Empty()
	inv.Boxes["dev"] = Box{Target: "smith@100.92.14.7"}

	got, err := Register(inv, "dev", "smith@198.51.100.7")
	if err == nil {
		t.Fatal("Register() err = nil, want a refusal for a name that already names another box")
	}
	var collision *NameCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("Register() err = %v, want a *NameCollisionError", err)
	}
	if collision.Name != "dev" || collision.Registered != "smith@100.92.14.7" {
		t.Errorf("collision = %+v, want it to carry the name and the target it is held by", collision)
	}
	if !strings.Contains(err.Error(), "dev") {
		t.Errorf("Register() err = %q, want it to name the box", err)
	}
	if got.Boxes["dev"].Target != "smith@100.92.14.7" {
		t.Errorf("Register() boxes = %v, want the registered box left as it was", got.Boxes)
	}
}

func TestRegisterRefusesAnEmptyName(t *testing.T) {
	if _, err := Register(Empty(), "", "smith@100.92.14.7"); err == nil {
		t.Fatal("Register() err = nil, want a refusal for an empty box name")
	}
}
