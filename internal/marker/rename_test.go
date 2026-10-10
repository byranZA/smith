package marker

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRenameRewritesOnlyTheName(t *testing.T) {
	t.Parallel()
	const before = `{
  "schema_version": 2,
  "smith_version": "0.9.1",
  "access_mode": "tailscale",
  "name": "a",
  "blueprint": "go-dev",
  "completed_phases": ["packages", "smith-user"],
  "updated_at": "2026-09-01T08:00:00Z"
}
`
	const want = `{
  "schema_version": 2,
  "smith_version": "0.9.1",
  "access_mode": "tailscale",
  "name": "b",
  "blueprint": "go-dev",
  "completed_phases": ["packages", "smith-user"],
  "updated_at": "2026-09-01T08:00:00Z"
}
`

	got, err := Rename([]byte(before), "b")
	if err != nil {
		t.Fatalf("Rename(marker, %q) error = %v", "b", err)
	}
	if string(got) != want {
		t.Errorf("Rename(marker, %q) = %q, want %q", "b", got, want)
	}
}

func TestRenameNamesAMarkerThatRecordedNoName(t *testing.T) {
	t.Parallel()
	const before = `{
  "schema_version": 1,
  "smith_version": "0.4.0",
  "access_mode": "public",
  "completed_phases": ["packages"],
  "updated_at": "2026-05-01T08:00:00Z"
}
`
	const want = `{
  "name": "b",
  "schema_version": 1,
  "smith_version": "0.4.0",
  "access_mode": "public",
  "completed_phases": ["packages"],
  "updated_at": "2026-05-01T08:00:00Z"
}
`

	got, err := Rename([]byte(before), "b")
	if err != nil {
		t.Fatalf("Rename(marker, %q) error = %v", "b", err)
	}
	if string(got) != want {
		t.Errorf("Rename(marker, %q) = %q, want %q", "b", got, want)
	}
}

func TestRenameRefusesWhatIsNotAMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
	}{
		{"malformed JSON", "{not json"},
		{"an array", `["name"]`},
		{"an empty object", `{}`},
		{"a truncated object", `{"schema_version": 2, "name": "a"`},
		{"a malformed field after the name", `{"name": "a", garbage}`},
		{"trailing data", `{"name": "a"} trailing`},
		{"a second object", `{"name": "a"} {"name": "c"}`},
		{"a duplicate name", `{"name": "a", "name": "a"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Rename([]byte(tt.data), "b"); !errors.Is(err, ErrMalformed) {
				t.Errorf("Rename(%q, %q) err = %v, want ErrMalformed", tt.data, "b", err)
			}
		})
	}
}

func TestRenameKeepsEveryByteButTheName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		data, want string
	}{
		{
			"an escaped name key",
			`{"schema_version": 2, "na\u006de": "a"}`,
			`{"schema_version": 2, "na\u006de": "b"}`,
		},
		{
			"unknown fields and a newer schema",
			`{"schema_version": 9, "future": {"name": "x", "list": [1,2]}, "name": "a", "z": null}` + "\n",
			`{"schema_version": 9, "future": {"name": "x", "list": [1,2]}, "name": "b", "z": null}` + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Rename([]byte(tt.data), "b")
			if err != nil {
				t.Fatalf("Rename(%q, %q) err = %v, want nil", tt.data, "b", err)
			}
			if string(got) != tt.want {
				t.Errorf("Rename(%q, %q) = %q, want %q", tt.data, "b", got, tt.want)
			}
		})
	}
}

func TestRenameKeepsTheDecoderErrorForDataThatIsNotJSON(t *testing.T) {
	t.Parallel()
	_, err := Rename([]byte("nope"), "b")
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || !errors.Is(err, ErrMalformed) {
		t.Errorf("Rename(%q, %q) err = %v, want ErrMalformed wrapping a *json.SyntaxError", "nope", "b", err)
	}
}
