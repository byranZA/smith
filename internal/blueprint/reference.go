package blueprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/byranZA/smith/internal/secret"
)

// The schemes a blueprint value may name. env: and file: point at a secret
// living somewhere else; literal: declares a plain value on purpose, because
// exported constants such as NODE_ENV or LOG_LEVEL are real and making an
// operator set a shell variable to export one would get the rule resented and
// worked around.
//
// This buys less than it looks like it buys, and pretending otherwise would be
// theatre: it prevents nothing, since anyone in a hurry can type
// literal:ghp_abc and commit a token. It buys two things. Committing a secret
// has to be done on purpose, and `grep -r 'literal:' ~/.smith/blueprints`
// finds every one of them in a second — which turns "is there a secret in this
// directory?" from a judgement call over every string in every file into a
// mechanical check a pre-commit hook can run.
//
// A placement source is the narrower set. Its reference resolves to a whole
// file's bytes, so literal: there would mean inline file content — a heredoc
// feature, and precisely the shape you least want a secret sitting in.
var (
	valueSchemes  = []string{"env", "file", "literal"}
	sourceSchemes = []string{"env", "file"}
)

// reference refuses a value that is not a reference to one of the schemes in
// known. The rule is that a value parses as a *known* scheme, not that it
// contains a colon, so LOG_FORMAT: "json:pretty" is reported as the unknown
// scheme it names rather than as baffling prose about references.
//
// Nothing here is resolved: no file is read and no environment variable is
// looked up, so a blueprint referring to a machine other than this one still
// validates. The split is secret.Split, so the grammar checked here and the
// grammar resolved later are the same one.
func reference(path, value string, known []string) []Finding {
	if value == "" {
		return nil
	}
	scheme, _, ok := secret.Split(value)
	switch {
	case !ok:
		return []Finding{{Path: path, Message: fmt.Sprintf(
			"this is not a reference: name where the value lives with %s, or to declare a plain value on purpose write it as %q",
			schemeList(known), "literal:<value>")}}
	case !slices.Contains(known, scheme):
		return []Finding{{Path: path, Message: fmt.Sprintf(
			"%q is not a reference scheme smith knows: %s", scheme, schemeList(known))}}
	default:
		return nil
	}
}

// envFindings checks every value in one env block, under the path the block
// was declared at. A map has lost the order the operator wrote it in, so the
// variables are reported by name in sorted order and the report stays the same
// from one run to the next.
func envFindings(path string, env map[string]string) []Finding {
	var report []Finding
	for _, name := range slices.Sorted(maps.Keys(env)) {
		report = append(report, reference(path+"."+name, env[name], valueSchemes)...)
	}
	return report
}

// source checks a placement's source reference, naming literal: as refused
// here rather than as unknown: the operator reaching for it wants inline file
// content, and being told the scheme does not exist would send them looking
// for a typo.
func source(path, from string) []Finding {
	if scheme, _, ok := secret.Split(from); ok && scheme == "literal" {
		return []Finding{{Path: path, Message: fmt.Sprintf(
			"%q is not valid as a placement source: a source resolves to a whole file's bytes, so point at one with %q or %q",
			"literal:", "file:", "env:")}}
	}
	return reference(path, from, sourceSchemes)
}

// schemeList renders the schemes a field accepts as prose, in the order they
// are declared, so the operator reads the whole set rather than a guess at
// which one they meant.
func schemeList(known []string) string {
	quoted := make([]string, len(known))
	for i, k := range known {
		quoted[i] = fmt.Sprintf("%q", k+":")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
