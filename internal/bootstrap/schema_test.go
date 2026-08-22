package bootstrap

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/byranZA/smith/internal/marker"
)

// scriptSchemaVersion matches the SMITH_BOOTSTRAP_VERSION assignment in
// bootstrap.sh — the schema version the box-side script stamps into the marker.
var scriptSchemaVersion = regexp.MustCompile(`(?m)^SMITH_BOOTSTRAP_VERSION=(\d+)`)

// TestScriptSchemaVersionMatchesTheMarkerPackage pins the two hand-kept sides of
// the marker in lockstep: the schema the embedded script writes and the schema
// this build understands. They are separate declarations by necessity — one is
// bash, one is Go — so a bump to either alone would have smith reading a marker
// as skewed against itself.
func TestScriptSchemaVersionMatchesTheMarkerPackage(t *testing.T) {
	m := scriptSchemaVersion.FindStringSubmatch(Script)
	if m == nil {
		t.Fatal("no SMITH_BOOTSTRAP_VERSION=<n> found in the embedded script")
	}
	got, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse SMITH_BOOTSTRAP_VERSION %q: %v", m[1], err)
	}
	if got != marker.SchemaVersion {
		t.Errorf("SMITH_BOOTSTRAP_VERSION = %d, want marker.SchemaVersion (%d)", got, marker.SchemaVersion)
	}
}
