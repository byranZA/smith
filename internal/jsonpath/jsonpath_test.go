package jsonpath_test

import (
	"encoding/json"
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
