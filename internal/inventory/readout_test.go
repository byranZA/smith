package inventory

import (
	"strings"
	"testing"
)

func TestReadoutListsBoxesInNameOrderWithTheirTargets(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"scratch": {Target: "smith@203.0.113.42"},
		"dev":     {Target: "smith@100.92.14.7"},
		"api":     {Target: "smith@100.92.14.31"},
	}}

	out := Readout(inv, SkewNone, nil)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("Readout() = %q, want a header, three boxes and a summary", out)
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "TARGET") {
		t.Errorf("header = %q, want NAME and TARGET columns", lines[0])
	}
	for i, want := range []string{"api", "dev", "scratch"} {
		if !strings.HasPrefix(lines[i+1], want) {
			t.Errorf("line %d = %q, want the box %q in name order", i+1, lines[i+1], want)
		}
	}
	if !strings.Contains(lines[1], "smith@100.92.14.31") {
		t.Errorf("line for api = %q, want it to carry the box's target", lines[1])
	}
	if last := lines[len(lines)-1]; last != "3 boxes" {
		t.Errorf("summary = %q, want %q", last, "3 boxes")
	}
}

func TestReadoutSummarisesASingleBox(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{"dev": {Target: "smith@100.92.14.7"}}}

	out := Readout(inv, SkewNone, nil)

	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "1 box") {
		t.Errorf("Readout() = %q, want it to end with the summary %q", out, "1 box")
	}
}

func TestReadoutSaysSoWhenNoBoxesAreRegistered(t *testing.T) {
	out := Readout(Empty(), SkewNone, nil)

	if !strings.Contains(out, "no boxes registered") {
		t.Errorf("Readout() = %q, want it to report that no boxes are registered", out)
	}
	if !strings.Contains(out, "smith machine add") {
		t.Errorf("Readout() = %q, want it to name the command that registers a box", out)
	}
	if strings.Contains(out, "NAME") {
		t.Errorf("Readout() = %q, want no column header with nothing to list", out)
	}
}

func TestReadoutNotesAnInventoryANewerSmithWrote(t *testing.T) {
	inv := Inventory{SchemaVersion: 99, Boxes: map[string]Box{"dev": {Target: "smith@100.92.14.7"}}}

	out := Readout(inv, SkewNewer, nil)

	if !strings.Contains(out, "dev") {
		t.Errorf("Readout() = %q, want the entries it recognises listed", out)
	}
	if !strings.Contains(out, "newer smith") {
		t.Errorf("Readout() = %q, want it to note that a newer smith wrote the file", out)
	}
}

func TestReadoutMarksEachBoxReachableOrUnreachableWhenProbed(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{
		"dev":     {Target: "smith@100.92.14.7"},
		"scratch": {Target: "smith@203.0.113.42"},
	}}

	out := Readout(inv, SkewNone, map[string]bool{"dev": true, "scratch": false})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[0], "REACHABLE") {
		t.Errorf("header = %q, want a REACHABLE column", lines[0])
	}
	if !strings.HasSuffix(lines[1], "reachable") || strings.HasSuffix(lines[1], "unreachable") {
		t.Errorf("line for dev = %q, want it marked reachable", lines[1])
	}
	if !strings.HasSuffix(lines[2], "unreachable") {
		t.Errorf("line for scratch = %q, want it marked unreachable", lines[2])
	}
}

func TestReadoutOmitsTheReachableColumnWhenNothingWasProbed(t *testing.T) {
	inv := Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{"dev": {Target: "smith@100.92.14.7"}}}

	out := Readout(inv, SkewNone, nil)

	if strings.Contains(out, "REACHABLE") {
		t.Errorf("Readout() = %q, want no REACHABLE column with no probe results", out)
	}
}
