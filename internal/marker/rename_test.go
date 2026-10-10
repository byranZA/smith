package marker

import "testing"

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
	for _, data := range []string{"{not json", `["name"]`, `{}`} {
		if _, err := Rename([]byte(data), "b"); err == nil {
			t.Errorf("Rename(%q, %q) error = nil, want an error", data, "b")
		}
	}
}
