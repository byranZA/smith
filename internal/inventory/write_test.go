package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRoundTripsThroughRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	inv := Empty()
	inv.Boxes["dev"] = Box{Target: "smith@100.92.14.7"}

	if err := Write(path, inv, SkewNone); err != nil {
		t.Fatalf("Write() err = %v, want nil", err)
	}
	got, skew, err := Read(path)
	if err != nil {
		t.Fatalf("Read() err = %v, want nil", err)
	}
	if got.Boxes["dev"].Target != "smith@100.92.14.7" {
		t.Errorf("Read() boxes = %v, want dev at smith@100.92.14.7", got.Boxes)
	}
	if skew != SkewNone {
		t.Errorf("Read() skew = %v, want SkewNone", skew)
	}
}

func TestWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boxes.json")

	if err := Write(path, Empty(), SkewNone); err != nil {
		t.Fatalf("Write() err = %v, want nil", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "boxes.json" {
		t.Errorf("directory holds %v, want only the inventory itself", entries)
	}
}

func TestWriteReplacesTheFileWholesale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	write(t, path, `{"schema_version":1,"boxes":{"old":{"target":"smith@203.0.113.10"}}}`)
	inv := Empty()
	inv.Boxes["dev"] = Box{Target: "smith@100.92.14.7"}

	if err := Write(path, inv, SkewNone); err != nil {
		t.Fatalf("Write() err = %v, want nil", err)
	}
	got, _, err := Read(path)
	if err != nil {
		t.Fatalf("Read() err = %v, want nil", err)
	}
	if _, ok := got.Boxes["old"]; ok {
		t.Errorf("Read() boxes = %v, want the file replaced by what was written", got.Boxes)
	}
}

func TestWriteRefusesAnInventoryANewerSmithOwns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")
	const content = `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`
	write(t, path, content)
	inv := Empty()
	inv.Boxes["dev"] = Box{Target: "smith@198.51.100.7"}

	err := Write(path, inv, SkewNewer)
	if err == nil {
		t.Fatal("Write() err = nil, want a refusal for an inventory a newer smith wrote")
	}
	if !strings.Contains(err.Error(), "upgrade smith") {
		t.Errorf("Write() err = %q, want it to name upgrading as the fix", err)
	}
	if got := read(t, path); got != content {
		t.Errorf("inventory = %q, want it byte-identical at %q", got, content)
	}
}

func TestWriteStampsTheSchemaVersionThisBuildWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boxes.json")

	if err := Write(path, Inventory{Boxes: map[string]Box{}}, SkewNone); err != nil {
		t.Fatalf("Write() err = %v, want nil", err)
	}
	got, _, err := Read(path)
	if err != nil {
		t.Fatalf("Read() err = %v, want nil", err)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, SchemaVersion)
	}
}

// read returns the file's content, failing the test if it cannot be read.
func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
