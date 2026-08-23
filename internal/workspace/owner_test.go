package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSmithUserLeavesItsOwnFileAlone proves the real account is
// check-before-change: a file already belonging to the account smith runs as
// is reported as needing no claim, which is what makes a converged re-run a
// no-op rather than a chown of every placement.
func TestSmithUserLeavesItsOwnFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	if err := os.WriteFile(path, []byte("declared\n"), 0o600); err != nil {
		t.Fatalf("write the destination: %v", err)
	}

	claimed, err := SmithUser{}.Claim(path)

	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if claimed {
		t.Errorf("Claim() = true, want false for a file the account already owns")
	}
}

// TestSmithUserRefusesAnAbsentDestination proves a claim on a file that is not
// there is reported by name rather than passing silently.
func TestSmithUserRefusesAnAbsentDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	if _, err := (SmithUser{}).Claim(path); err == nil {
		t.Errorf("Claim() error = nil, want a refusal naming %s", path)
	}
}
