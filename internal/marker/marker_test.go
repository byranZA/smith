package marker

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := Marker{
		SchemaVersion:   SchemaVersion,
		SmithVersion:    "1.2.3",
		AccessMode:      "public",
		CompletedPhases: []string{"packages", "smith-user"},
		UpdatedAt:       "2026-07-19T12:00:00Z",
	}

	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	got, skew, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if skew != SkewNone {
		t.Errorf("Decode() skew = %v, want SkewNone", skew)
	}
	if got.SmithVersion != in.SmithVersion || got.AccessMode != in.AccessMode || got.UpdatedAt != in.UpdatedAt {
		t.Errorf("Decode() = %+v, want %+v", got, in)
	}
	if strings.Join(got.CompletedPhases, ",") != strings.Join(in.CompletedPhases, ",") {
		t.Errorf("CompletedPhases = %v, want %v", got.CompletedPhases, in.CompletedPhases)
	}
}

func TestDecodeMalformedIsError(t *testing.T) {
	if _, _, err := Decode([]byte("{not json")); err == nil {
		t.Error("Decode(malformed) error = nil, want an error")
	}
}

func TestDecodeSchemaSkew(t *testing.T) {
	tests := []struct {
		name           string
		schema         int
		wantSkew       Skew
		wantUnderstood bool
	}{
		{"current schema is understood cleanly", SchemaVersion, SkewNone, true},
		{"newer schema is not understood", SchemaVersion + 1, SkewNewer, false},
		{"zero or missing schema is not understood", 0, SkewNewer, false},
	}
	// An older-but-known schema only exists once the schema has advanced past 1;
	// include that case only when it is reachable.
	if SchemaVersion > 1 {
		tests = append(tests, struct {
			name           string
			schema         int
			wantSkew       Skew
			wantUnderstood bool
		}{"older known schema is understood with a note", SchemaVersion - 1, SkewOlder, true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A newer schema may carry fields this build does not model; it must
			// still decode to the raw facts it recognizes.
			m := Marker{SchemaVersion: tt.schema, AccessMode: "public", CompletedPhases: []string{"packages"}}
			data, err := Encode(m)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			got, skew, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if skew != tt.wantSkew {
				t.Errorf("skew = %v, want %v", skew, tt.wantSkew)
			}
			if skew.Understood() != tt.wantUnderstood {
				t.Errorf("Understood() = %v, want %v", skew.Understood(), tt.wantUnderstood)
			}
			if got.AccessMode != "public" {
				t.Errorf("raw facts lost: AccessMode = %q, want %q", got.AccessMode, "public")
			}
			if skew == SkewNewer && !strings.Contains(strings.ToLower(skew.Note()), "upgrade") {
				t.Errorf("SkewNewer Note() = %q, want it to tell the operator to upgrade smith", skew.Note())
			}
		})
	}
}

func TestAppendPhaseRecordsOnSuccess(t *testing.T) {
	m := Marker{AccessMode: "public"}

	m = AppendPhase(m, "packages")
	if len(m.CompletedPhases) != 1 || m.CompletedPhases[0] != "packages" {
		t.Fatalf("CompletedPhases = %v, want [packages]", m.CompletedPhases)
	}

	m = AppendPhase(m, "smith-user")
	if strings.Join(m.CompletedPhases, ",") != "packages,smith-user" {
		t.Errorf("CompletedPhases = %v, want [packages smith-user] in order", m.CompletedPhases)
	}
}

func TestAppendPhaseIsIdempotent(t *testing.T) {
	m := AppendPhase(Marker{}, "packages")
	m = AppendPhase(m, "packages")
	if len(m.CompletedPhases) != 1 {
		t.Errorf("re-appending a satisfied phase duplicated it: %v", m.CompletedPhases)
	}
}

func TestAppendPhaseDoesNotMutateInput(t *testing.T) {
	original := Marker{CompletedPhases: []string{"packages"}}
	_ = AppendPhase(original, "smith-user")
	if len(original.CompletedPhases) != 1 {
		t.Errorf("AppendPhase mutated its input: %v", original.CompletedPhases)
	}
}
