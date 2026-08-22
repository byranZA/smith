package inventory

import (
	"strings"
	"testing"
)

func TestDecodeReadsBoxesAndTheirTargets(t *testing.T) {
	data := []byte(`{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	inv, skew, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() err = %v, want nil", err)
	}
	if skew != SkewNone {
		t.Errorf("Decode() skew = %v, want SkewNone", skew)
	}
	if got := inv.Boxes["dev"].Target; got != "smith@100.92.14.7" {
		t.Errorf("Decode() target for dev = %q, want %q", got, "smith@100.92.14.7")
	}
}

func TestEncodeRoundTripsWithoutLoss(t *testing.T) {
	inv := Inventory{
		SchemaVersion: SchemaVersion,
		Boxes: map[string]Box{
			"dev":     {Target: "smith@100.92.14.7"},
			"scratch": {Target: "smith@203.0.113.42"},
		},
	}

	data, err := Encode(inv)
	if err != nil {
		t.Fatalf("Encode() err = %v, want nil", err)
	}
	got, skew, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode(Encode()) err = %v, want nil", err)
	}
	if skew != SkewNone {
		t.Errorf("skew = %v, want SkewNone", skew)
	}
	if len(got.Boxes) != len(inv.Boxes) {
		t.Fatalf("round-tripped %d boxes, want %d", len(got.Boxes), len(inv.Boxes))
	}
	for name, box := range inv.Boxes {
		if got.Boxes[name] != box {
			t.Errorf("round-tripped box %q = %+v, want %+v", name, got.Boxes[name], box)
		}
	}
	if got.SchemaVersion != inv.SchemaVersion {
		t.Errorf("round-tripped schema version = %d, want %d", got.SchemaVersion, inv.SchemaVersion)
	}
}

func TestDecodeRefusesMalformedJSON(t *testing.T) {
	if _, _, err := Decode([]byte("{not json")); err == nil {
		t.Fatal("Decode() err = nil, want an error for malformed JSON")
	}
}

func TestDecodeClassifiesSchemaSkew(t *testing.T) {
	tests := []struct {
		name string
		data string
		want Skew
	}{
		{"current schema", `{"schema_version":1,"boxes":{}}`, SkewNone},
		{"newer schema", `{"schema_version":99,"boxes":{}}`, SkewNewer},
		{"absent schema", `{"boxes":{}}`, SkewNewer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, skew, err := Decode([]byte(tt.data))
			if err != nil {
				t.Fatalf("Decode() err = %v, want nil", err)
			}
			if skew != tt.want {
				t.Errorf("Decode() skew = %v, want %v", skew, tt.want)
			}
		})
	}
}

func TestDecodeKeepsTheEntriesANewerSchemaStillCarries(t *testing.T) {
	data := []byte(`{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7","future":"x"}}}`)

	inv, skew, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() err = %v, want nil", err)
	}
	if skew.Understood() {
		t.Errorf("Understood() = true, want false for a newer schema")
	}
	if got := inv.Boxes["dev"].Target; got != "smith@100.92.14.7" {
		t.Errorf("target for dev = %q, want the entry a newer file still carries", got)
	}
	if !strings.Contains(skew.Note(), "newer smith") {
		t.Errorf("Note() = %q, want it to say a newer smith wrote the file", skew.Note())
	}
}
