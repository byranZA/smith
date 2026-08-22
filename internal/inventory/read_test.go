package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTreatsAnAbsentFileAsAnEmptyInventory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "boxes.json")

	inv, skew, err := Read(path)
	if err != nil {
		t.Fatalf("Read() err = %v, want nil for an absent inventory", err)
	}
	if len(inv.Boxes) != 0 {
		t.Errorf("Read() boxes = %v, want none", inv.Boxes)
	}
	if skew != SkewNone {
		t.Errorf("Read() skew = %v, want SkewNone", skew)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("Stat(cache dir) err = %v, want the directory not to have been created", err)
	}
}

func TestReadDecodesTheFileOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	inv, _, err := Read(path)
	if err != nil {
		t.Fatalf("Read() err = %v, want nil", err)
	}
	if got := inv.Boxes["dev"].Target; got != "smith@100.92.14.7" {
		t.Errorf("Read() target for dev = %q, want %q", got, "smith@100.92.14.7")
	}
}

func TestReadRefusesAMalformedFileNamingItsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, "{not json")

	_, _, err := Read(path)
	if err == nil {
		t.Fatal("Read() err = nil, want an error for a malformed inventory")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Read() err = %q, want it to name %s", err, path)
	}
}

// write puts content at path, creating the parent directory.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadForSkipsTheFileEntirelyForALiteralTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, "{not json")

	inv, skew, err := LoadFor(path, "smith@203.0.113.10")
	if err != nil {
		t.Fatalf("LoadFor() err = %v, want nil — a literal target is never looked up", err)
	}
	if len(inv.Boxes) != 0 {
		t.Errorf("LoadFor() boxes = %v, want none", inv.Boxes)
	}
	if skew != SkewNone {
		t.Errorf("LoadFor() skew = %v, want SkewNone", skew)
	}
}

func TestLoadForReadsTheInventoryForABareValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	inv, _, err := LoadFor(path, "dev")
	if err != nil {
		t.Fatalf("LoadFor() err = %v, want nil", err)
	}
	if got := Resolve(inv, "dev"); got != "smith@100.92.14.7" {
		t.Errorf("Resolve after LoadFor = %q, want %q", got, "smith@100.92.14.7")
	}
}

func TestLoadForRefusesAMalformedInventoryForABareValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, "{not json")

	if _, _, err := LoadFor(path, "dev"); err == nil {
		t.Fatal("LoadFor() err = nil, want an error for a malformed inventory")
	}
}
