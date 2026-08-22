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
	"regexp"
	"slices"
	"strings"

	"github.com/byranZA/smith/internal/jsonpath"
)

// The placeholders smith substitutes into a template. These four are the whole
// vocabulary: every other argument is the operator's literal text, passed
// through as written, and a placeholder outside this set is a validation error
// rather than a literal sent to the provider.
const (
	// namePlaceholder is the operator-chosen box name.
	namePlaceholder = "{{name}}"
	// markerArgPlaceholder is the value create stamps the box with.
	markerArgPlaceholder = "{{marker_arg}}"
	// sshKeyPlaceholder is the operator's provider-side key reference.
	sshKeyPlaceholder = "{{ssh_key}}"
	// idPlaceholder is the box id, supplied to the destroy template, which v1
	// accepts as data and never executes.
	idPlaceholder = "{{id}}"
)

// placeholders is the set smith supplies, in the order an error lists them.
var placeholders = []string{namePlaceholder, markerArgPlaceholder, sshKeyPlaceholder, idPlaceholder}

// placeholderPattern matches any {{...}} token in a template argument, so an
// unrecognised one is caught rather than passed to the provider verbatim.
var placeholderPattern = regexp.MustCompile(`{{[^{}]*}}`)

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
	if err := Validate(adapter); err != nil {
		return Box{}, err
	}
	argv := render(adapter.Create, map[string]string{
		namePlaceholder:      name,
		markerArgPlaceholder: adapter.Marker.Arg,
		sshKeyPlaceholder:    adapter.SSHKey,
	})
	doc, err := run(ctx, runner, argv)
	if err != nil {
		return Box{}, err
	}
	return extract(doc, adapter.Record.Create, adapter.Extract)
}

// Validate reports whether smith can run an adapter's templates: it must
// declare a create template, and every {{placeholder}} in any template must be
// one of the four smith supplies. An unrecognised placeholder is caught here
// rather than sent to the provider as literal text, which is what a template
// carrying {{sshkey}} would otherwise become.
func Validate(adapter Adapter) error {
	if len(adapter.Create) == 0 {
		return fmt.Errorf("provider adapter declares no create template")
	}
	templates := []struct {
		name string
		argv []string
	}{
		{"create", adapter.Create},
		{"list", adapter.List},
		{"destroy", adapter.Destroy},
	}
	for _, template := range templates {
		for _, arg := range template.argv {
			for _, found := range placeholderPattern.FindAllString(arg, -1) {
				if !slices.Contains(placeholders, found) {
					return fmt.Errorf("provider adapter's %s template uses unknown placeholder %s: smith supplies %s",
						template.name, found, strings.Join(placeholders, ", "))
				}
			}
		}
	}
	return nil
}

// render substitutes smith's placeholders into a template, leaving every other
// argument exactly as the operator wrote it, and drops the arguments left
// empty.
//
// An argument that renders empty is dropped along with the argument before it
// when that one is a flag. A flat argument list has no other way to say "omit
// this": an operator who declares no ssh key would otherwise send
// --ssh-key "", which the provider CLI rejects. The rule is mechanical and the
// same for every placeholder, which is what makes an adapter whose marker is
// the box's own name — no create argument at all — a configuration rather than
// a special case here.
func render(template []string, values map[string]string) []string {
	argv := make([]string, 0, len(template))
	for _, arg := range template {
		rendered := arg
		for placeholder, value := range values {
			rendered = strings.ReplaceAll(rendered, placeholder, value)
		}
		if rendered == "" && rendered != arg {
			argv = dropFlag(argv)
			continue
		}
		argv = append(argv, rendered)
	}
	return argv
}

// dropFlag removes the argument a dropped value was passed behind, when that
// argument is a flag. A preceding argument that is not a flag is the
// provider's own literal text — a subcommand, say — and stays.
func dropFlag(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	if last := argv[len(argv)-1]; !strings.HasPrefix(last, "-") || last == "-" {
		return argv
	}
	return argv[:len(argv)-1]
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
