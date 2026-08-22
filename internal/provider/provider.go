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
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/byranZA/smith/internal/provider/extractpath"
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
	// valuePlaceholder is the operator-chosen box name, and it belongs to the
	// marker's own fields rather than to a template: the marker's only job is
	// answering "did smith create this box", and the box already has a name.
	valuePlaceholder = "{{value}}"
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

// Marker is how a smith-created box is stamped at the provider and recognised
// again: what create passes, where the extractor reads it back from, and the
// form it reads back in. Three fields, because all three genuinely differ
// across providers — an hcloud label is a key/value pair written as
// "smith={{value}}" and read back from "labels.smith" as the bare name, while
// a doctl tag is an opaque string that reads back exactly as it was written.
//
// Whether the marker is a label, a tag, or the box's own name is the adapter's
// business: an adapter whose token lacks tag permission stamps nothing and
// reads the marker from the box's name, and smith runs it through the same code
// with no branch.
//
// Read and Expect have no consumer in v1: the marker was to be teardown's
// reconciliation key, and destroy is out of scope, and provider-side discovery
// is out of scope too. They are accepted and validated so adapters written
// today stay correct when discovery ships — smith writes a marker at create and
// never reads one back.
type Marker struct {
	// Arg is the value create stamps the box with, rendered with {{value}}
	// bound to the box name. Empty stamps nothing.
	Arg string
	// Read is the path the marker is read back from in the provider's JSON.
	// Nothing reads it in v1.
	Read string
	// Expect is the form the marker reads back in, rendered with {{value}}
	// bound to the box name. Nothing reads it in v1.
	Expect string
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
// provider's own identifier for the box and its public address. An address the
// provider has not assigned yet is empty.
//
// The marker a box carries is not a field here: v1 stamps a marker on create
// and never reads one back, so carrying it would hand every caller a field that
// is always empty.
type Box struct {
	// ID is the provider's own identifier for the box.
	ID string
	// IP is the box's public address, empty when the provider reported none.
	IP string
}

// Create runs the adapter's create template for a box called name and returns
// the box the provider reported. It creates the box and stops there: nothing is
// written to the box and no bootstrap runs, because a create that chained into
// setup and failed halfway would leave the operator holding a box that exists,
// is billed, and that smith cannot tear down.
//
// A provider that assigns an address later answers create with an id and no
// address, and Create then polls the list template until the entry whose id
// matches carries one — which is why it takes a clock.
func Create(ctx context.Context, runner Runner, clock Clock, adapter Adapter, name string) (Box, error) {
	if err := Validate(adapter); err != nil {
		return Box{}, err
	}
	if err := requireEnv(adapter.Requires); err != nil {
		return Box{}, err
	}
	argv := render(adapter.Create, map[string]string{
		namePlaceholder:      name,
		markerArgPlaceholder: strings.ReplaceAll(adapter.Marker.Arg, valuePlaceholder, name),
		sshKeyPlaceholder:    adapter.SSHKey,
	})
	doc, err := run(ctx, runner, argv)
	if err != nil {
		return Box{}, err
	}
	box, err := extract(doc, adapter.Record.Create, adapter.Extract)
	if err != nil {
		return Box{}, err
	}
	if box.IP != "" {
		return box, nil
	}
	return awaitAddress(ctx, runner, clock, adapter, box)
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
			if found, ok := unknown(arg, placeholders); ok {
				return fmt.Errorf("provider adapter's %s template uses unknown placeholder %s: smith supplies %s",
					template.name, found, strings.Join(placeholders, ", "))
			}
		}
	}
	fields := []struct {
		name  string
		value string
	}{
		{"arg", adapter.Marker.Arg},
		{"expect", adapter.Marker.Expect},
	}
	for _, field := range fields {
		if found, ok := unknown(field.value, []string{valuePlaceholder}); ok {
			return fmt.Errorf("provider adapter's marker %s uses unknown placeholder %s: smith supplies %s",
				field.name, found, valuePlaceholder)
		}
	}
	return nil
}

// unknown reports the first {{placeholder}} in text that is not one smith
// supplies here. A template and a marker field draw on different vocabularies,
// so the accepted set is a parameter.
func unknown(text string, accepted []string) (string, bool) {
	for _, found := range placeholderPattern.FindAllString(text, -1) {
		if !slices.Contains(accepted, found) {
			return found, true
		}
	}
	return "", false
}

// requireEnv checks that every environment variable the adapter declares is
// present, before any provider command runs, so a missing credential fails with
// smith's own message rather than whatever the provider CLI says about it.
//
// Presence is the whole check, and deliberately so on both sides:
//
// It guards a missing credential and never an insufficient one. A token can be
// present, correctly named, and still lack the scope the template needs — a
// DigitalOcean token without tag permission passes here and is refused only at
// create, with "403 ... missing the required permission tag:create". No
// pre-flight smith can run distinguishes the two.
//
// And the list is optional: hcloud contexts hold the token in
// ~/.config/hcloud/cli.toml with no environment variable at all, so an adapter
// declaring nothing is checked for nothing and trusts the CLI's own credential
// story.
//
// smith reads the name and never the value. The value is never resolved,
// staged, logged or carried into an error message; the subprocess inherits the
// operator's environment and picks it up there.
func requireEnv(names []string) error {
	var missing []string
	for _, name := range names {
		if _, ok := os.LookupEnv(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("provider requires %s in the environment: set %s and run the command again",
		quoteAll(missing), plural(len(missing), "it", "them"))
}

// quoteAll renders names as a quoted, comma-separated list for an error.
func quoteAll(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return strings.Join(quoted, ", ")
}

// plural picks the singular or plural word for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
		found, err := extractpath.Lookup(doc, record)
		if err != nil {
			return Box{}, fmt.Errorf("provider adapter's record path: %w", err)
		}
		doc = found
	}
	return extractRecord(doc, paths)
}

// extractRecord reads one located box record into the canonical form. The
// record path has already been applied, so the extractor paths are read
// relative to the record itself.
func extractRecord(doc any, paths Extract) (Box, error) {
	id, err := extractpath.Lookup(doc, paths.ID)
	if err != nil {
		return Box{}, fmt.Errorf("provider adapter's id path: %w", err)
	}
	ip, err := extractpath.Lookup(doc, paths.IP)
	if err != nil {
		return Box{}, fmt.Errorf("provider adapter's ip path: %w", err)
	}
	return Box{ID: extractpath.Text(id), IP: extractpath.Text(ip)}, nil
}
