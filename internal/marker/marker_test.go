package marker

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	if _, _, err := Decode([]byte("{not json")); err == nil {
		t.Error("Decode(malformed) error = nil, want an error")
	}
}

func TestDecodeSchemaSkew(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := AppendPhase(Marker{}, "packages")
	m = AppendPhase(m, "packages")
	if len(m.CompletedPhases) != 1 {
		t.Errorf("re-appending a satisfied phase duplicated it: %v", m.CompletedPhases)
	}
}

func TestAppendPhaseDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	original := Marker{CompletedPhases: []string{"packages"}}
	_ = AppendPhase(original, "smith-user")
	if len(original.CompletedPhases) != 1 {
		t.Errorf("AppendPhase mutated its input: %v", original.CompletedPhases)
	}
}

// TestMarkerRecordsTheBlueprintPointerAndNoContentHash pins the blueprint
// pointer: the marker records which blueprint a box was built from, by name
// only. A content hash would be stale the moment either side is edited — the
// staged document is ground truth — so the record must carry nothing about the
// document's contents.
func TestMarkerRecordsTheBlueprintPointerAndNoContentHash(t *testing.T) {
	t.Parallel()
	data, err := Encode(Marker{SchemaVersion: SchemaVersion, AccessMode: "public", Blueprint: "acme"})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, _, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Blueprint != "acme" {
		t.Errorf("Blueprint = %q, want %q", got.Blueprint, "acme")
	}
	for _, unwanted := range []string{"hash", "digest", "sha"} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("marker JSON contains %q, want the blueprint name and nothing about the document's contents:\n%s", unwanted, data)
		}
	}
}

// TestMarkerRecordsTheBoxName pins the box's self-record of the
// operator-chosen name: without it the name lives only in the operator's box
// inventory, so an entry rebuilt from the box comes back as an IP address and
// every command that names the box breaks.
func TestMarkerRecordsTheBoxName(t *testing.T) {
	t.Parallel()
	data, err := Encode(Marker{SchemaVersion: SchemaVersion, AccessMode: "public", Name: "dev"})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, skew, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if skew != SkewNone {
		t.Errorf("skew = %v, want SkewNone", skew)
	}
	if got.Name != "dev" {
		t.Errorf("Name = %q, want %q", got.Name, "dev")
	}
}

// TestEncodeOmitsAnUnnamedBoxAndAnAbsentBlueprint proves a marker never carries
// an empty name or blueprint key: a box built from no blueprint records none,
// rather than recording one that is the empty string.
func TestEncodeOmitsAnUnnamedBoxAndAnAbsentBlueprint(t *testing.T) {
	t.Parallel()
	data, err := Encode(Marker{SchemaVersion: SchemaVersion, AccessMode: "public"})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	for _, key := range []string{`"name"`, `"blueprint"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("marker JSON carries %s with no value to record:\n%s", key, data)
		}
	}
}

// TestPreviousSchemaMarkerKeepsItsFacts proves a marker written at the previous
// schema version still decodes to the facts this build recognizes — as an
// older-but-understood schema, with the fields that schema never had empty —
// so every existing consumer keeps working against a box smith set up before
// the schema advanced.
func TestPreviousSchemaMarkerKeepsItsFacts(t *testing.T) {
	t.Parallel()
	const v1 = `{
  "schema_version": 1,
  "smith_version": "0.9.0",
  "access_mode": "public",
  "blueprint": "",
  "completed_phases": ["packages", "smith-user"],
  "updated_at": "2026-07-19T12:00:00Z"
}`
	got, skew, err := Decode([]byte(v1))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if skew != SkewOlder {
		t.Errorf("skew = %v, want SkewOlder", skew)
	}
	if !skew.Understood() {
		t.Error("Understood() = false, want a v1 marker to stay understood")
	}
	if got.Name != "" || got.Blueprint != "" {
		t.Errorf("Name = %q, Blueprint = %q, want both empty on a schema that never had them", got.Name, got.Blueprint)
	}
	if got.AccessMode != "public" || got.SmithVersion != "0.9.0" {
		t.Errorf("raw facts lost: %+v", got)
	}
	if strings.Join(got.CompletedPhases, ",") != "packages,smith-user" {
		t.Errorf("CompletedPhases = %v, want [packages smith-user]", got.CompletedPhases)
	}
}
