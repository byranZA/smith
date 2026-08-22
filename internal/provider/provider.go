// Package provider creates boxes at a cloud provider through an
// operator-supplied adapter.
//
// An adapter is data, not code: command templates plus field extractors, all
// emitting JSON, which smith renders, runs and normalizes to the canonical box
// record. smith embeds no provider SDK and holds no provider knowledge — image,
// size and region are the operator's own literal text inside their own
// template, and the only values smith substitutes are the ones it actually
// knows.
//
// The command runner is passed in rather than constructed here, because
// launching a provider CLI is a genuine system boundary: a caller supplies a
// box name and gets back a box or an error, and a test supplies a fake runner
// and never touches a real provider.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/jsonpath"
)

// namePlaceholder is the operator-chosen box name's placeholder in a template.
// It is the only value smith substitutes into a create template in this form;
// every other argument is the operator's literal text, passed through as
// written.
const namePlaceholder = "{{name}}"

// Runner launches a local process. It is the narrow command-running interface
// the provider CLI is reached through, deliberately the same shape
// connection.Exec has, so the one real implementation serves both.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Adapter is the decoded provider block: the operator's description of one
// provider's CLI. smith renders and runs its templates and never interprets
// what is in them.
type Adapter struct {
	// Create is the argv template that creates a box.
	Create []string
	// List is the argv template that lists the account's boxes.
	List []string
	// Destroy is the argv template that tears a box down. v1 accepts it as
	// data and never executes it.
	Destroy []string
	// Requires names the environment variables the templates need present.
	Requires []string
	// SSHKey names a key already registered at the provider.
	SSHKey string
	// Marker is how a smith-created box is stamped and recognised again.
	Marker Marker
	// Record is where the box record sits inside a template's response.
	Record Record
	// Extract is how a box's identity and address are read out of the
	// provider's JSON.
	Extract Extract
}

// Marker is how a smith-created box is stamped at the provider and read back
// again: what create passes, and where the extractor reads it from. The two
// differ per provider, which is why they are separate fields.
type Marker struct {
	// Arg is the value create stamps the box with.
	Arg string
	// Read is the path the marker is read back from in the provider's JSON.
	Read string
}

// Record is the path locating the box record inside a template's response.
// It is per template rather than per adapter because one provider wraps the
// two differently: hcloud's create answers with the record under "server" and
// its list answers with a bare array.
//
// An empty path means the response is the record.
type Record struct {
	// Create is the path to the record in the create template's response.
	Create string
	// List is the path to the box records in the list template's response.
	List string
}

// Extract is the paths a box's identity and address are read from in the
// provider's JSON.
type Extract struct {
	// ID is the path to the provider's own identifier for the box.
	ID string
	// IP is the path to the box's public address.
	IP string
}

// Box is the canonical record smith normalizes any provider's JSON to: the
// provider's own identifier for the box, its public address, and the marker it
// carries. An address the provider has not assigned yet is empty.
type Box struct {
	// ID is the provider's own identifier for the box.
	ID string
	// IP is the box's public address, empty when the provider reported none.
	IP string
	// Marker is the provider-side marker the box carries.
	Marker string
}

// Create runs the adapter's create template for a box called name and returns
// the box the provider reported. It creates the box and stops there: nothing is
// written to the box and no bootstrap runs, because a create that chained into
// setup and failed halfway would leave the operator holding a box that exists,
// is billed, and that smith cannot tear down.
func Create(ctx context.Context, runner Runner, adapter Adapter, name string) (Box, error) {
	if len(adapter.Create) == 0 {
		return Box{}, fmt.Errorf("provider adapter declares no create template")
	}
	argv := render(adapter.Create, name)
	doc, err := run(ctx, runner, argv)
	if err != nil {
		return Box{}, err
	}
	return extract(doc, adapter.Record.Create, adapter.Extract)
}

// render substitutes the box name into a template, leaving every other
// argument exactly as the operator wrote it.
func render(template []string, name string) []string {
	argv := make([]string, len(template))
	for i, arg := range template {
		argv[i] = strings.ReplaceAll(arg, namePlaceholder, name)
	}
	return argv
}

// run launches the rendered argv and decodes its stdout as JSON. A failing
// command carries the provider's own error text out, since smith knows nothing
// about what the provider refused and the operator needs to read it verbatim.
func run(ctx context.Context, runner Runner, argv []string) (any, error) {
	var stdout, stderr bytes.Buffer
	if err := runner.Run(ctx, argv[0], argv[1:], nil, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("provider command %q failed: %w%s", strings.Join(argv, " "), err, providerOutput(stderr.String()))
	}
	dec := json.NewDecoder(&stdout)
	// Provider ids are JSON integers: decoded as float64 a large one renders in
	// exponent notation and no longer matches the box it names.
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("provider command %q wrote output that could not be read as JSON: %w", argv[0], err)
	}
	return doc, nil
}

// providerOutput renders the provider's own error text for an error message, or
// nothing at all when the provider said nothing.
func providerOutput(stderr string) string {
	if strings.TrimSpace(stderr) == "" {
		return ""
	}
	return "\n" + strings.TrimRight(stderr, "\n")
}

// extract normalizes a provider's response to the canonical box record. The
// record path locates the box within whatever envelope the provider wrapped it
// in; the extractor paths are then read relative to the record, so an adapter
// says where the box is once rather than repeating the envelope in every field.
func extract(doc any, record string, paths Extract) (Box, error) {
	if record != "" {
		found, err := jsonpath.Lookup(doc, record)
		if err != nil {
			return Box{}, fmt.Errorf("provider adapter's record path: %w", err)
		}
		doc = found
	}
	id, err := jsonpath.Lookup(doc, paths.ID)
	if err != nil {
		return Box{}, fmt.Errorf("provider adapter's id path: %w", err)
	}
	ip, err := jsonpath.Lookup(doc, paths.IP)
	if err != nil {
		return Box{}, fmt.Errorf("provider adapter's ip path: %w", err)
	}
	return Box{ID: text(id), IP: text(ip)}, nil
}

// text renders an extracted JSON value as the string the canonical record
// holds. A null is the empty string: a provider that has not assigned an
// address yet reports null, which is an absent value rather than a bad one.
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}
