package staging

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/marker"
)

func TestPointerRefusesABlueprintlessRerunNamingTheBlueprint(t *testing.T) {
	t.Parallel()

	err := Pointer(marker.Marker{Blueprint: "acme"}, "")
	if err == nil {
		t.Fatal("Pointer() err = nil, want a box built from a blueprint refused a blueprint-less re-run")
	}
	if !strings.Contains(err.Error(), "acme") {
		t.Errorf("Pointer() err = %v, want it to name the blueprint the box was built from", err)
	}
}

func TestPointerAllowsARunAgainstABoxThatRecordsNoBlueprint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		m    marker.Marker
	}{
		{"a marker recording no blueprint", marker.Marker{SchemaVersion: 2, AccessMode: "public"}},
		{"no marker at all", marker.Marker{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := Pointer(tt.m, ""); err != nil {
				t.Errorf("Pointer() err = %v, want a blueprint-less run to be allowed", err)
			}
		})
	}
}

func TestPointerAllowsARunThatNamesABlueprint(t *testing.T) {
	t.Parallel()

	if err := Pointer(marker.Marker{Blueprint: "acme"}, "other"); err != nil {
		t.Errorf("Pointer() err = %v, want a run naming a blueprint to be allowed", err)
	}
}
