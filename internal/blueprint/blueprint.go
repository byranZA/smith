// Package blueprint models the reusable declaration of a kind of dev box —
// which repos it carries, which runtimes and packages it installs, which files
// land where, how it is reached, which terminal it runs — and turns the bytes
// of a blueprint file into that declaration.
//
// Callers hand Parse the bytes of a blueprint file and get back either a valid
// document or an error describing what is wrong with it. The document format
// is an implementation detail: no YAML type appears in this package's public
// interface, so the format can change without touching a caller.
//
// Validation is strict. A key the schema does not define is an error, never a
// warning — a blueprint is desired state, and a desired-state document that
// quietly ignores what the operator wrote is the worst available failure. On
// top of the strict structural pass sit the semantic rules that catch a
// document parsing cleanly while meaning nothing: a repo with no remote, two
// repos that would collide in the workspace, a field whose value is outside
// the set smith recognises, a schema field name misindented into one of the
// open maps, where it becomes a phantom tool or environment variable, and a
// placement destination that disagrees with the scope it was declared in.
package blueprint

import (
	"bytes"
	"errors"
	"io"
	"sort"

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
	// Git is the identity the box commits as.
	Git Git `yaml:"git"`
	// Provider is the adapter smith creates and lists boxes through. It is
	// absent when the operator declares no provider, which is what lets a
	// blueprint inherit an operator-wide adapter whole rather than in part.
	Provider *Provider `yaml:"provider"`
	// Packages are the system packages installed on the box.
	Packages []string `yaml:"packages"`
	// Tools are the box-wide toolchain versions, keyed by tool name.
	Tools map[string]string `yaml:"tools"`
	// Env are the box-wide environment variables, keyed by variable name.
	// Every value is a reference, never a literal secret.
	Env map[string]string `yaml:"env"`
	// Placements are the files put on the box outside any worktree.
	Placements []Placement `yaml:"placements"`
	// Repos are the repositories the box carries.
	Repos []Repo `yaml:"repos"`
}

// Git is the git identity smith writes on the box. Its fields are snake_case
// in the document, matching the placeholder style of the provider block.
type Git struct {
	// UserName is the name commits are authored under.
	UserName string `yaml:"user_name"`
	// UserEmail is the address commits are authored under.
	UserEmail string `yaml:"user_email"`
}

// Provider is the operator-supplied description of one provider's CLI:
// command templates plus field extractors, all emitting JSON. It is data, not
// code — this package decodes it and never executes any part of it.
type Provider struct {
	// Create is the argv template that creates a box.
	Create []string `yaml:"create"`
	// List is the argv template that lists the account's boxes.
	List []string `yaml:"list"`
	// Destroy is the argv template that tears a box down.
	Destroy []string `yaml:"destroy"`
	// Requires names the environment variables the templates need present.
	Requires []string `yaml:"requires"`
	// SSHKey names a key already registered at the provider, substituted into
	// Create. An unset key drops the argument rather than passing an empty one.
	SSHKey string `yaml:"ssh_key"`
	// Marker is how a smith-created box is stamped and recognised again.
	Marker ProviderMarker `yaml:"marker"`
	// Extract is how a box's identity and address are read out of the
	// provider's JSON.
	Extract ProviderExtract `yaml:"extract"`
}

// ProviderMarker is the two halves of a provider marker: what create passes
// and what the extractor reads back, which differ across providers.
type ProviderMarker struct {
	// Arg is the value create stamps the box with.
	Arg string `yaml:"arg"`
	// Read is the path the marker is read back from in the provider's JSON.
	Read string `yaml:"read"`
}

// ProviderExtract is the paths a box's identity and address are read from in
// the provider's JSON.
type ProviderExtract struct {
	// ID is the path to the provider's own identifier for the box.
	ID string `yaml:"id"`
	// IP is the path to the box's public address.
	IP string `yaml:"ip"`
}

// Placement puts one file on the box. Its scope comes from where it is
// declared: a placement under a repo is worktree-relative, one at the top
// level is box-wide.
type Placement struct {
	// From is a reference to the source bytes, resolved operator-side.
	From string `yaml:"from"`
	// To is the destination path.
	To string `yaml:"to"`
	// Mode is how the file is maintained: "converge" or "once".
	Mode string `yaml:"mode"`
	// Perms is the destination's octal file mode, such as "0600".
	Perms string `yaml:"perms"`
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
	// Tools are the toolchain versions this repo pins over the box-wide ones.
	Tools map[string]string `yaml:"tools"`
	// Env are the environment variables scoped to this repo.
	Env map[string]string `yaml:"env"`
	// Placements are the files put inside this repo's worktree.
	Placements []Placement `yaml:"placements"`
}

// Parse decodes a blueprint document, refusing any key the schema does not
// define and any value smith would not know what to do with. An empty document
// is a valid blueprint with every field unset. A repo that names no worktree
// directory is given the last segment of its clone URL.
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
	var structural []Finding
	switch err := dec.Decode(&b); {
	case err == nil, errors.Is(err, io.EOF):
	default:
		var schema *yaml.TypeError
		if !errors.As(err, &schema) {
			return Blueprint{}, malformedReport(err)
		}
		// The decoder fills every field it did understand, so the semantic
		// rules still run over the rest of the document and the operator sees
		// one report rather than a queue of them.
		structural = schemaFindings(schema, indexPaths(&doc))
	}

	// The reserved-name rule reads the document rather than the decoded
	// blueprint, so its findings carry a line and are merged with the
	// structural ones by position.
	positioned := append(structural, reservedFindings(&doc)...)
	sort.SliceStable(positioned, func(i, j int) bool { return positioned[i].Line < positioned[j].Line })

	b = withDefaults(b)
	if report := append(positioned, validate(b)...); len(report) > 0 {
		return Blueprint{}, &ValidationError{Findings: report}
	}
	return b, nil
}
