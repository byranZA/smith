package inventory

import "testing"

func TestNamePrefersTheNameTheOperatorPassed(t *testing.T) {
	name, origin := Name(Naming{Flag: "dev", Marker: "old", Blueprint: "acme", Host: "203.0.113.10"})

	if name != "dev" {
		t.Errorf("Name() = %q, want the name the operator passed", name)
	}
	if origin != OriginFlag {
		t.Errorf("Name() origin = %v, want OriginFlag", origin)
	}
}

func TestNameFallsBackThroughMarkerBlueprintAndHost(t *testing.T) {
	tests := []struct {
		name       string
		naming     Naming
		wantName   string
		wantOrigin Origin
	}{
		{
			name:       "the marker names a box the operator did not",
			naming:     Naming{Marker: "dev", Blueprint: "acme", Host: "203.0.113.10"},
			wantName:   "dev",
			wantOrigin: OriginMarker,
		},
		{
			name:       "the blueprint names a box with no marker name",
			naming:     Naming{Blueprint: "acme", Host: "203.0.113.10"},
			wantName:   "acme",
			wantOrigin: OriginBlueprint,
		},
		{
			name:       "the host names a box with no blueprint",
			naming:     Naming{Host: "203.0.113.10"},
			wantName:   "203.0.113.10",
			wantOrigin: OriginHost,
		},
		{
			name:       "nothing names a box smith knows nothing about",
			naming:     Naming{},
			wantName:   "",
			wantOrigin: OriginNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, origin := Name(tt.naming)
			if name != tt.wantName {
				t.Errorf("Name() = %q, want %q", name, tt.wantName)
			}
			if origin != tt.wantOrigin {
				t.Errorf("Name() origin = %v, want %v", origin, tt.wantOrigin)
			}
		})
	}
}

func TestNameNeverInventsASuffixedName(t *testing.T) {
	name, _ := Name(Naming{Blueprint: "acme", Host: "198.51.100.7"})

	if name != "acme" {
		t.Errorf("Name() = %q, want the blueprint's name exactly, never an invented one", name)
	}
}
