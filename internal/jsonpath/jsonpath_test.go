package jsonpath_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/jsonpath"
)

// decode reads a JSON document the way smith does — numbers preserved exactly,
// never round-tripped through float64.
func decode(t *testing.T, doc string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return out
}

func TestLookupReadsAValue(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
		want string
	}{
		{"top-level key", `{"id": 42}`, "id", "42"},
		{"dotted descent", `{"public_net": {"ipv4": {"ip": "203.0.113.10"}}}`, "public_net.ipv4.ip", "203.0.113.10"},
		{"large integer keeps its exact value", `{"id": 519823476123}`, "id", "519823476123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonpath.Lookup(decode(t, tt.doc), tt.path)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v", tt.path, err)
			}
			if text := toString(got); text != tt.want {
				t.Errorf("Lookup(%q) = %q, want %q", tt.path, text, tt.want)
			}
		})
	}
}

// toString renders a looked-up value the way a test asserts on it.
func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func TestLookupReportsAPathThatMatchedNothing(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
	}{
		{"absent key", `{"id": 42}`, "droplet.id"},
		{"descent through a scalar", `{"id": 42}`, "id.value"},
		{"empty path", `{"id": 42}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jsonpath.Lookup(decode(t, tt.doc), tt.path)
			if err == nil {
				t.Fatalf("Lookup(%q) err = nil, want an error naming the path", tt.path)
			}
			if tt.path != "" && !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Lookup(%q) err = %v, want it to name the path", tt.path, err)
			}
		})
	}
}

func TestLookupTreatsANullAsAValue(t *testing.T) {
	got, err := jsonpath.Lookup(decode(t, `{"tags": null}`), "tags")
	if err != nil {
		t.Fatalf("Lookup(\"tags\") err = %v, want a null to be a value, not a missing field", err)
	}
	if got != nil {
		t.Errorf("Lookup(\"tags\") = %v, want nil", got)
	}
}

// fixture decodes a real provider response captured from a live run.
func fixture(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return decode(t, string(raw))
}

func TestLookupSelectsEveryElementOfAnArray(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
		want string
	}{
		{"the first element carries the value", `[{"id": 1}, {"id": 2}]`, "[*].id", "1"},
		{"a later element carries it", `[{"other": 1}, {"id": 2}]`, "[*].id", "2"},
		{"the array is under a key", `{"servers": [{"id": 7}]}`, "servers[*].id", "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonpath.Lookup(decode(t, tt.doc), tt.path)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v", tt.path, err)
			}
			if text := toString(got); text != tt.want {
				t.Errorf("Lookup(%q) = %q, want %q", tt.path, text, tt.want)
			}
		})
	}
}

// The address list's ordering is not stable: in the same DigitalOcean account
// the public address came second on one droplet and first on another. The same
// path must yield the public address either way, or smith takes a 10.x address
// and the box is unreachable for a reason that looks like anything but a path
// bug.
func TestLookupPredicateSelectsByFieldValueNotByPosition(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"public entry first", "[name=smith-proto-REDACTED].networks.v4[type=public].ip_address", "203.0.113.10"},
		{"private entry first", "[name=an-unrelated-droplet].networks.v4[type=public].ip_address", "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonpath.Lookup(fixture(t, "doctl-list"), tt.path)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v", tt.path, err)
			}
			if text := toString(got); text != tt.want {
				t.Errorf("Lookup(%q) = %q, want %q", tt.path, text, tt.want)
			}
		})
	}
}

func TestLookupLocatesTheRecordInWhateverEnvelopeTheProviderUses(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		record  string
		want    string
	}{
		{"an array response", "doctl-create", "[*]", "593069736"},
		{"an object envelope", "hcloud-create", "server", "55512345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := jsonpath.Lookup(fixture(t, tt.fixture), tt.record)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v", tt.record, err)
			}
			got, err := jsonpath.Lookup(record, "id")
			if err != nil {
				t.Fatalf("Lookup(\"id\") err = %v", err)
			}
			if text := toString(got); text != tt.want {
				t.Errorf("id of %s = %q, want %q", tt.fixture, text, tt.want)
			}
		})
	}
}

// doctl reports an unset tag list as null rather than as an empty array, so the
// marker path a working adapter declares has to survive it.
func TestLookupTreatsANullCollectionAsAbsentRatherThanAMiss(t *testing.T) {
	for _, path := range []string{"[*].tags", "[*].tags[*]", "[*].tags[type=public]"} {
		t.Run(path, func(t *testing.T) {
			got, err := jsonpath.Lookup(fixture(t, "doctl-create"), path)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v, want a null collection to be absent, not a miss", path, err)
			}
			if got != nil {
				t.Errorf("Lookup(%q) = %v, want nil", path, got)
			}
		})
	}
}

func TestLookupReportsAFilterThatMatchedNothing(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		path string
	}{
		{"no element satisfies the predicate", `[{"type": "private"}]`, "[type=public].ip"},
		{"every element lacks the key", `[{"other": 1}]`, "[*].id"},
		{"the value filtered is not an array", `{"id": 42}`, "id[*]"},
		{"an empty array has no elements", `{"tags": []}`, "tags[*]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jsonpath.Lookup(decode(t, tt.doc), tt.path)
			if err == nil {
				t.Fatalf("Lookup(%q) err = nil, want an error naming the path", tt.path)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Lookup(%q) err = %v, want it to name the path", tt.path, err)
			}
		})
	}
}

func TestLookupRefusesAFilterItDoesNotUnderstand(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"a positional index", "networks.v4[0].ip_address"},
		{"an unclosed bracket", "networks.v4[type=public"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jsonpath.Lookup(fixture(t, "doctl-create"), tt.path)
			if err == nil {
				t.Fatalf("Lookup(%q) err = nil, want the filter refused", tt.path)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Lookup(%q) err = %v, want it to name the path", tt.path, err)
			}
		})
	}
}

// The three adapters the prototype ran read their marker from three different
// places — a label map, an opaque tag list, and the box's own name — and the
// same grammar reaches all three with no special case. If one of these ever
// needs its own handling, provider knowledge has leaked into smith.
func TestLookupReadsEveryMarkerFormTheAdaptersUse(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		path    string
		want    string
	}{
		{"a label map", "hcloud-list", "[*].labels.smith", "abc"},
		{"the box's own name", "doctl-list", "[*].name", "smith-proto-REDACTED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonpath.Lookup(fixture(t, tt.fixture), tt.path)
			if err != nil {
				t.Fatalf("Lookup(%q) err = %v", tt.path, err)
			}
			if text := toString(got); text != tt.want {
				t.Errorf("Lookup(%q) = %q, want %q", tt.path, text, tt.want)
			}
		})
	}
}
