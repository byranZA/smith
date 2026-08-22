// Package inventory models smith's box inventory: the ~/.smith/cache/boxes.json
// address book mapping an operator-chosen box name to the SSH target smith
// proved works.
//
// It holds identity and nothing else. Access mode, completed phases, the
// blueprint pointer and the provider destroy reference all live on the box's
// marker, so there is never a second answer to a question the box already
// answers — and so there is nothing here that can silently go stale. A wrong
// target fails loudly on connect; a wrong cached access mode would not.
//
// The file is the operator's machine's, never a box's: a box knows of no other
// boxes. Reading never creates it — an absent inventory is an empty inventory,
// which is what lets listing and name resolution work before any box exists. A
// malformed one is refused rather than treated as empty, because treating it as
// empty would invite the next write to overwrite entries smith could not read.
package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SchemaVersion is the inventory schema version this build of smith writes and
// fully understands. It is versioned independently of the marker's schema: the
// two files are written by different machines and change for different reasons.
const SchemaVersion = 1

// Inventory is the decoded box inventory: a schema version and the boxes smith
// knows how to reach, keyed by their operator-chosen names.
type Inventory struct {
	// SchemaVersion is the inventory schema this file was written with.
	SchemaVersion int `json:"schema_version"`
	// Boxes maps each box's operator-chosen name to what smith knows about it.
	Boxes map[string]Box `json:"boxes"`
}

// Box is one entry in the inventory: a box's identity, and nothing more.
type Box struct {
	// Target is the opaque SSH target smith proved reaches the box. It is never
	// parsed — smith hands it to ssh verbatim, so an ssh_config alias, a
	// MagicDNS name and a bare address all work with no smith code.
	Target string `json:"target"`
}

// Empty returns an inventory holding no boxes, stamped with the schema version
// this build writes. It is what reading an absent file yields, so a caller
// never has to tell "no inventory" from "an inventory with nothing in it".
func Empty() Inventory {
	return Inventory{SchemaVersion: SchemaVersion, Boxes: map[string]Box{}}
}

// Encode renders inv as the indented JSON written to the inventory file.
func Encode(inv Inventory) ([]byte, error) {
	if inv.Boxes == nil {
		inv.Boxes = map[string]Box{}
	}
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode inventory: %w", err)
	}
	return data, nil
}

// Decode parses inventory JSON into an Inventory and classifies its schema skew
// against this build. Malformed JSON is an error. A well-formed inventory whose
// schema this build does not fully understand still decodes to the entries it
// recognizes; the returned Skew tells the caller how far to trust them.
func Decode(data []byte) (Inventory, Skew, error) {
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return Inventory{}, SkewNewer, fmt.Errorf("decode inventory: %w", err)
	}
	if inv.Boxes == nil {
		inv.Boxes = map[string]Box{}
	}
	return inv, classify(inv.SchemaVersion), nil
}

// Read decodes the inventory at path. An absent file is an empty inventory and
// no error — reading never creates the config home, and every read path has to
// work before the first box is registered. A malformed file is an error naming
// the path, so the operator is told which file to fix.
func Read(path string) (Inventory, Skew, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the inventory lives at a path smith derives itself.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Empty(), SkewNone, nil
		}
		return Inventory{}, SkewNewer, fmt.Errorf("read inventory %s: %w", path, err)
	}
	inv, skew, err := Decode(data)
	if err != nil {
		return Inventory{}, skew, fmt.Errorf("inventory %s: %w", path, err)
	}
	return inv, skew, nil
}

// LoadFor returns the inventory the value arg is resolved against. A value
// containing "@" is a literal ssh target, so nothing is read and an empty
// inventory is returned: an absent, unreadable or malformed boxes.json can
// never stand between the operator and a box they addressed directly. Any
// other value is looked up, so the inventory at path is read as it stands.
//
// It is the read half of the rule Resolve decides — both halves live here, so
// a verb gets them by calling this and Resolve rather than by re-deriving when
// the file may be skipped.
func LoadFor(path, arg string) (Inventory, Skew, error) {
	if literal(arg) {
		return Empty(), SkewNone, nil
	}
	return Read(path)
}

// Skew classifies a decoded inventory's schema_version against the version this
// build of smith understands. It mirrors the marker's classification rather than
// inventing a second vocabulary for the same idea.
type Skew int

const (
	// SkewNone means the inventory's schema matches this build exactly.
	SkewNone Skew = iota
	// SkewOlder means an older but still-understood schema: smith proceeds and
	// notes it.
	SkewOlder
	// SkewNewer means a newer or unrecognized schema: smith does not migrate it
	// and tells the operator to upgrade, trusting only the entries it decoded.
	SkewNewer
)

// classify maps an inventory's schema version to its skew against this build.
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

// Understood reports whether this build can rely on an inventory at this skew:
// true for the current and older-known schemas, false once the schema is newer
// or unrecognized.
func (s Skew) Understood() bool { return s == SkewNone || s == SkewOlder }

// Note returns a human-readable note for the skew, or "" when the schema
// matches this build and there is nothing to say.
func (s Skew) Note() string {
	switch s {
	case SkewOlder:
		return "inventory written by an older smith schema; proceeding"
	case SkewNewer:
		return "inventory written by a newer smith; upgrade smith — listing the entries it recognises"
	default:
		return ""
	}
}
