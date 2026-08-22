// Package blueprint models the reusable declaration of a kind of dev box —
// which repos it carries, how it is reached, which terminal it runs — and
// turns the bytes of a blueprint file into that declaration.
//
// Callers hand Parse the bytes of a blueprint file and get back either a valid
// document or an error describing what is wrong with it. The document format
// is an implementation detail: no YAML type appears in this package's public
// interface, so the format can change without touching a caller.
//
// Validation is strict. A key the schema does not define is an error, never a
// warning — a blueprint is desired state, and a desired-state document that
// quietly ignores what the operator wrote is the worst available failure.
package blueprint

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"
)

// Blueprint is a parsed blueprint document: the desired state of one kind of
// box. One blueprint instantiates many boxes.
type Blueprint struct {
	// Access is how the box is reached: "public" or "tailscale".
	Access string `yaml:"access"`
	// Terminal is the terminal substrate sessions run under.
	Terminal string `yaml:"terminal"`
	// Workspace is the directory on the box the repo worktrees live under.
	Workspace string `yaml:"workspace"`
	// Repos are the repositories the box carries.
	Repos []Repo `yaml:"repos"`
}

// Repo is one repository a box carries, cloned into the workspace.
type Repo struct {
	// Name is the worktree directory name. It defaults to the last path
	// segment of URL when the operator leaves it out.
	Name string `yaml:"name"`
	// URL is the clone URL. It is the one field a repo cannot omit.
	URL string `yaml:"url"`
	// Base is the branch worktrees start from.
	Base string `yaml:"base"`
}

// Parse decodes a blueprint document, refusing any key the schema does not
// define. An empty document is a valid blueprint with every field unset.
//
// Every problem in the document is reported in one pass: the returned error is
// a *ValidationError carrying an ordered list of findings, so an operator
// fixing a blueprint sees the whole list rather than the first line of it. A
// document that is not YAML at all is reported as malformed instead, with the
// position the parser gave up at.
func Parse(data []byte) (Blueprint, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Blueprint{}, malformedReport(err)
	}
	if doc.Kind == 0 {
		// A document holding nothing to decode is an empty blueprint, which is
		// valid, not malformed.
		return Blueprint{}, nil
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var b Blueprint
	err := dec.Decode(&b)
	if err == nil || errors.Is(err, io.EOF) {
		return b, nil
	}
	var schema *yaml.TypeError
	if errors.As(err, &schema) {
		return Blueprint{}, schemaReport(schema, indexPaths(&doc))
	}
	return Blueprint{}, malformedReport(err)
}
