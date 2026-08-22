// Package staging owns the /etc/smith/ config contract: the layout `machine
// setup` writes onto a box and on-box smith reads back.
//
// A box learns what kind of box it is from the operator's blueprint, and the
// only machine that can read that blueprint is the operator's own. So `machine
// setup` stages the document onto the box — verbatim, byte for byte, with no
// field filtered and no derived format — and on-box smith reads what was staged
// for it rather than fetching configuration of its own.
//
// The package is split into pure seams and an applying one. Plan derives the
// desired staged tree from a blueprint document and touches nothing; Resolve
// turns each placement's source reference into the bytes it names, which only
// the operator's machine can do; Converge applies that tree to a box over a
// narrow connection, comparing digests before it writes so a re-run that
// changes nothing rewrites nothing. Load is the read half, running on the box:
// it re-validates the staged document through the same strict parser and
// refuses one it cannot trust rather than running a verb against a
// half-understood blueprint.
//
// A placement's staged path is a pure function of its scope and destination,
// exported so the writer and the on-box reader derive it the same way and
// cannot disagree about where a blob lives.
//
// /etc/smith/ is a provisioned box's state. The box is deliberately given no
// ~/.smith/ — that is the operator's config home, and the two must never be the
// same directory.
package staging

import (
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
)

// Root is the directory on the box holding everything `machine setup` stages.
// The marker already lives here; the staged config joins it.
const Root = "/etc/smith"

// DocumentPath is where the operator's blueprint is staged on the box. It is a
// committed, non-secret artifact, so it is world-readable: an operator who
// SSHes in can read the document their box was built from.
const DocumentPath = Root + "/" + documentFile

// documentMode is the staged document's permissions, and documentOwner its
// owner: root, because `machine setup` is the sole writer.
const (
	documentMode  = "0644"
	documentOwner = "root:root"
)

// PlacementsDir is the directory on the box holding the bytes of every
// placement the blueprint declares. It is smith-owned and 0700: the operator
// resolved these on their own machine and they are provisioned secrets, so
// nothing but the account that consumes them can read the directory.
const PlacementsDir = Root + "/" + placementsSubdir

// The staged tree's fixed shape. The writer and the reader both derive their
// paths from these, so the two cannot disagree about where a blob lives.
const (
	// placementsSubdir is the placements directory's name under Root.
	placementsSubdir = "placements"
	// boxSubdir is where box-scoped placements are keyed, beside the
	// repo-scoped ones.
	boxSubdir = "box"
	// boxHome is the smith user's home on the box, which a box destination's
	// leading ~/ canonicalizes to.
	boxHome = "/home/smith"
	// smithOwner is the smith user and group, as chown spells it.
	smithOwner = "smith:smith"
	// placementsMode is the placements directory's permissions.
	placementsMode = "0700"
	// defaultPerms is what a placement carries when it declares none. A
	// provisioned secret is readable by its consumer and nobody else.
	defaultPerms = "0600"
	// rootMode is the box config directory's permissions, and rootOwner its
	// owner: the marker and the document live here and both are readable.
	rootMode  = "0755"
	rootOwner = "root:root"
)

// File is one file of the staged tree: where it lands on the box, the bytes it
// holds, and the mode and owner it carries once in place.
type File struct {
	// Path is the absolute path the file occupies on the box.
	Path string
	// Bytes is the file's content, exactly as it is written.
	Bytes []byte
	// Mode is the octal permission the staged file carries, as chmod spells it.
	Mode string
	// Owner is the user:group the staged file belongs to, as chown spells it.
	Owner string
}

// Dir is one directory of the staged tree, with the mode and owner it is to
// carry. A directory is ensured whether or not it holds anything, so a
// blueprint declaring no placements still leaves an empty placements directory
// rather than none at all.
type Dir struct {
	// Path is the absolute path the directory occupies on the box.
	Path string
	// Mode is the octal permission the directory carries, as install spells it.
	Mode string
	// Owner is the user:group the directory belongs to, as chown spells it.
	Owner string
}

// Placement is one declared placement of the staged tree: the source reference
// it comes from on the operator's machine, the destination it will eventually
// land at on the box, and the staged file holding its bytes in the meantime.
//
// From is opaque provenance once staged. It resolves only on the operator's
// machine, so it is carried for reporting and never read on the box.
type Placement struct {
	// From is the source reference, resolved operator-side.
	From string
	// Destination is where the file eventually lands, as the blueprint
	// declared it.
	Destination string
	// File is where the placement's bytes are staged, keyed by its scope and
	// destination. Its bytes are empty until Resolve attaches them.
	File File
}

// Tree is the staged config a blueprint declares: what /etc/smith/ is to hold
// once `machine setup` has converged it.
type Tree struct {
	// Dirs are the directories the tree occupies, outermost first.
	Dirs []Dir
	// Document is the operator's blueprint, staged verbatim.
	Document File
	// Placements are the declared placements and where their bytes are staged.
	Placements []Placement
}

// Plan derives the staged tree from the bytes of the operator's blueprint and
// the declaration those bytes parsed into. It is pure: it reads nothing, writes
// nothing, resolves no source reference, and reaches no box.
//
// The document is planned verbatim rather than re-rendered from the parsed
// declaration. On-box smith runs the same parser, so a derived format would buy
// nothing and could only lose a field the operator wrote. The declaration is
// taken as well because a placement's staged path is a function of its scope
// and destination, which only the parsed form carries.
func Plan(document []byte, b blueprint.Blueprint) Tree {
	tree := Tree{
		Dirs: []Dir{
			{Path: Root, Mode: rootMode, Owner: rootOwner},
			{Path: PlacementsDir, Mode: placementsMode, Owner: smithOwner},
		},
		Document: File{
			Path:  DocumentPath,
			Bytes: document,
			Mode:  documentMode,
			Owner: documentOwner,
		},
	}
	if len(b.Placements) > 0 {
		tree.Dirs = append(tree.Dirs, Dir{Path: boxDirIn(Root), Mode: placementsMode, Owner: smithOwner})
	}
	for _, p := range b.Placements {
		tree.Placements = append(tree.Placements, Placement{
			From:        p.From,
			Destination: p.To,
			File: File{
				Path:  BoxPlacementPathIn(Root, p.To),
				Mode:  perms(p.Perms),
				Owner: smithOwner,
			},
		})
	}
	return tree
}

// BoxPlacementPathIn is where a box placement's bytes are staged under a box
// config directory: the box scope's directory, then the destination flattened
// into a single name.
//
// It is the whole of the box-scoped key derivation and it is pure, so the
// writer that stages the bytes and the on-box reader that looks them up call
// the same function and cannot disagree about where a blob lives. A destination
// is canonicalized before it is flattened, so ~/.npmrc and /home/smith/.npmrc
// key identically rather than staging the same file twice.
func BoxPlacementPathIn(root, destination string) string {
	return filepath.Join(boxDirIn(root), flatten(canonicalBoxDestination(destination)))
}

// boxDirIn is the box scope's directory under a box config directory.
func boxDirIn(root string) string {
	return filepath.Join(root, placementsSubdir, boxSubdir)
}

// canonicalBoxDestination expands a box destination's leading ~/ to the smith
// user's home, so the two spellings of one path are one destination.
func canonicalBoxDestination(destination string) string {
	if rest, ok := strings.CutPrefix(destination, "~/"); ok {
		return boxHome + "/" + rest
	}
	return destination
}

// flatten encodes a destination into a single path segment: % first, then /,
// so the encoding is reversible and no two destinations collide. Nothing else
// is transformed, which keeps the staged tree greppable and lets a human
// reading it see what lands where.
func flatten(destination string) string {
	return strings.ReplaceAll(strings.ReplaceAll(destination, "%", "%25"), "/", "%2F")
}

// perms is the mode a staged placement carries: the one the operator declared,
// or the owner-only default when they declared none.
func perms(declared string) string {
	if declared == "" {
		return defaultPerms
	}
	return declared
}
