package staging

import "fmt"

// Resolver turns a placement's source reference into the value it names. It is
// the seam onto internal/secret's resolver: staging never reads a file or an
// environment variable itself, so every scheme that package learns is a scheme
// a placement can be sourced from, and a test can hand in a resolver of its
// own.
type Resolver func(ref string) (string, error)

// Resolve attaches each placement's bytes to the planned tree, returning a tree
// of its own rather than filling in the one it was given.
//
// Every source resolves on the operator's machine and nowhere else:
// file:/home/op/.secrets/npmrc means nothing on a VPS, so the bytes are what
// travel and the reference stays behind as provenance. Resolution happens
// before Converge writes anything, so a reference smith cannot resolve refuses
// the run while the box is still untouched.
func Resolve(tree Tree, resolve Resolver) (Tree, error) {
	placements := make([]Placement, len(tree.Placements))
	copy(placements, tree.Placements)
	for i, p := range placements {
		value, err := resolve(p.From)
		if err != nil {
			return Tree{}, fmt.Errorf("resolve placement %s from %s: %w", p.Destination, p.From, err)
		}
		placements[i].File.Bytes = []byte(value)
	}
	tree.Placements = placements
	return tree, nil
}
