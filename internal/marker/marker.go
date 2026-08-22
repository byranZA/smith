// Package marker models smith's on-box bootstrap marker: the
// /etc/smith/bootstrap.json ledger that records what setup did to a box.
//
// The marker is a ledger, not a gate. It records the schema and smith version
// that wrote it, how the box is reached, the phases completed in order, and
// when it was last written — so any admin machine can re-run setup idempotently
// and `smith machine status` can read the box's history back. It never decides
// whether a phase runs: every setup run executes all phases and
// check-before-change makes satisfied ones no-ops.
//
// bootstrap.sh writes the marker on the box; this package decodes it and
// classifies schema skew. When a marker was written by a newer smith than this
// build understands, smith does not migrate it — it degrades to the raw facts
// it recognizes and tells the operator to upgrade.
package marker

import (
	"encoding/json"
	"fmt"
)

// SchemaVersion is the marker schema version this build of smith writes and
// fully understands. bootstrap.sh stamps the same number into schema_version,
// so the two sides stay in lockstep.
const SchemaVersion = 1

// Marker is the decoded /etc/smith/bootstrap.json ledger.
type Marker struct {
	// SchemaVersion is the marker schema this record was written with.
	SchemaVersion int `json:"schema_version"`
	// SmithVersion is the smith build that wrote the record.
	SmithVersion string `json:"smith_version"`
	// AccessMode is how the box is reached: "public" or "tailscale".
	AccessMode string `json:"access_mode"`
	// Blueprint is the blueprint pointer: the name of the blueprint the box was
	// built from, empty when it was built from none. It is a name and nothing
	// else — no content hash — because the staged document is ground truth and
	// a hash would be stale the moment either side is edited.
	Blueprint string `json:"blueprint"`
	// CompletedPhases lists the phases that completed, in the order they ran.
	CompletedPhases []string `json:"completed_phases"`
	// UpdatedAt is when the marker was last written, as an RFC 3339 timestamp.
	UpdatedAt string `json:"updated_at"`
}

// Encode renders m as the indented JSON written to /etc/smith/bootstrap.json.
func Encode(m Marker) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode marker: %w", err)
	}
	return data, nil
}

// Decode parses marker JSON into a Marker and classifies its schema skew
// against this build. Malformed JSON is an error. A well-formed marker whose
// schema this build does not fully understand still decodes to the raw facts it
// recognizes; the returned Skew tells the caller how far to trust them.
func Decode(data []byte) (Marker, Skew, error) {
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, SkewNewer, fmt.Errorf("decode marker: %w", err)
	}
	return m, classify(m.SchemaVersion), nil
}

// AppendPhase returns a copy of m with phase recorded as completed. It is the
// append-phase-on-success operation, and is idempotent: a phase already present
// is not duplicated, so a re-run that re-completes a satisfied phase converges.
// It does not mutate m.
func AppendPhase(m Marker, phase string) Marker {
	for _, p := range m.CompletedPhases {
		if p == phase {
			return m
		}
	}
	m.CompletedPhases = append(append([]string(nil), m.CompletedPhases...), phase)
	return m
}

// Skew classifies a decoded marker's schema_version against the version this
// build of smith understands.
type Skew int

const (
	// SkewNone means the marker's schema matches this build exactly.
	SkewNone Skew = iota
	// SkewOlder means an older but still-understood schema: smith proceeds and
	// notes it.
	SkewOlder
	// SkewNewer means a newer or unrecognized schema: smith does not migrate it
	// and tells the operator to upgrade, trusting only the raw facts it decoded.
	SkewNewer
)

// classify maps a marker's schema version to its skew against this build.
func classify(schema int) Skew {
	switch {
	case schema == SchemaVersion:
		return SkewNone
	case schema > 0 && schema < SchemaVersion:
		return SkewOlder
	default:
		return SkewNewer
	}
}

// Understood reports whether this build can rely on a marker at this skew: true
// for the current and older-known schemas, false once the schema is newer or
// unrecognized.
func (s Skew) Understood() bool { return s == SkewNone || s == SkewOlder }

// Note returns a human-readable note for the skew, or "" when the schema
// matches this build and there is nothing to say.
func (s Skew) Note() string {
	switch s {
	case SkewOlder:
		return "marker written by an older smith schema; proceeding"
	case SkewNewer:
		return "marker written by a newer smith; upgrade smith — reading raw facts only"
	default:
		return ""
	}
}
