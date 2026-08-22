// Package staging owns the /etc/smith/ config contract: the layout `machine
// setup` writes onto a box and on-box smith reads back.
//
// A box learns what kind of box it is from the operator's blueprint, and the
// only machine that can read that blueprint is the operator's own. So `machine
// setup` stages the document onto the box — verbatim, byte for byte, with no
// field filtered and no derived format — and on-box smith reads what was staged
// for it rather than fetching configuration of its own.
//
// The package is split into a pure seam and an applying one. Plan derives the
// desired staged tree from a blueprint document and touches nothing; Converge
// applies that tree to a box over a narrow connection, comparing digests before
// it writes so a re-run that changes nothing rewrites nothing.
//
// /etc/smith/ is a provisioned box's state. The box is deliberately given no
// ~/.smith/ — that is the operator's config home, and the two must never be the
// same directory.
package staging

// Root is the directory on the box holding everything `machine setup` stages.
// The marker already lives here; the staged config joins it.
const Root = "/etc/smith"

// DocumentPath is where the operator's blueprint is staged on the box. It is a
// committed, non-secret artifact, so it is world-readable: an operator who
// SSHes in can read the document their box was built from.
const DocumentPath = Root + "/blueprint.yaml"

// documentMode is the staged document's permissions, and documentOwner its
// owner: root, because `machine setup` is the sole writer.
const (
	documentMode  = "0644"
	documentOwner = "root:root"
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

// Tree is the staged config a blueprint declares: what /etc/smith/ is to hold
// once `machine setup` has converged it.
type Tree struct {
	// Document is the operator's blueprint, staged verbatim.
	Document File
}

// Plan derives the staged tree from the bytes of the operator's blueprint. It
// is pure: it reads nothing, writes nothing, and reaches no box.
//
// The document is planned verbatim rather than re-rendered from the parsed
// declaration. On-box smith runs the same parser, so a derived format would buy
// nothing and could only lose a field the operator wrote.
func Plan(document []byte) Tree {
	return Tree{Document: File{
		Path:  DocumentPath,
		Bytes: document,
		Mode:  documentMode,
		Owner: documentOwner,
	}}
}
