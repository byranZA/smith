package blueprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The four shapes the YAML decoder reports a schema violation in. Every one of
// them names a Go type, so none of them is ever shown to an operator: each is
// matched here and rewritten into smith's own vocabulary.
var (
	unknownFieldRe = regexp.MustCompile(`^field (\S+) not found in type \S+$`)
	repeatFieldRe  = regexp.MustCompile(`^field (\S+) already set in type \S+$`)
	repeatKeyRe    = regexp.MustCompile(`^mapping key (.+) already defined at line (\d+)$`)
	wrongShapeRe   = regexp.MustCompile("^cannot unmarshal (!!\\S+)(?: `.*`)? into (.+)$")
	// positionRe splits the decoder's syntax-error prefix off its message.
	positionRe = regexp.MustCompile(`^yaml: (?:line (\d+): )?(.*)$`)
	// findingLineRe pulls the line number off one schema violation.
	findingLineRe = regexp.MustCompile(`^line (\d+): (.*)$`)
)

// malformedReport turns a syntax error — the document is not YAML at all —
// into a report carrying the position the parser gave up at.
func malformedReport(err error) *ValidationError {
	message, line := err.Error(), 0
	if m := positionRe.FindStringSubmatch(message); m != nil {
		line = atoi(m[1])
		message = m[2]
	}
	return &ValidationError{
		Malformed: true,
		Findings:  []Finding{{Line: line, Message: message}},
	}
}

// schemaReport turns the decoder's list of schema violations into findings in
// the operator's vocabulary, ordered by the line they appear on. A violation
// the decoder words in a shape smith does not recognise still carries its
// position, never the Go type the decoder named.
func schemaReport(err *yaml.TypeError, paths pathIndex) *ValidationError {
	findings := make([]Finding, 0, len(err.Errors))
	for _, raw := range err.Errors {
		findings = append(findings, translate(raw, paths))
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })
	return &ValidationError{Findings: findings}
}

// translate rewrites one decoder message as a finding.
func translate(raw string, paths pathIndex) Finding {
	line, message := 0, raw
	if m := findingLineRe.FindStringSubmatch(raw); m != nil {
		line = atoi(m[1])
		message = m[2]
	}
	switch {
	case unknownFieldRe.MatchString(message):
		field := unknownFieldRe.FindStringSubmatch(message)[1]
		return Finding{Line: line, Path: paths.container[line], Message: fmt.Sprintf("unknown field %q", field)}
	case repeatFieldRe.MatchString(message):
		field := repeatFieldRe.FindStringSubmatch(message)[1]
		return Finding{Line: line, Path: paths.container[line], Message: fmt.Sprintf("field %q is declared twice", field)}
	case repeatKeyRe.MatchString(message):
		m := repeatKeyRe.FindStringSubmatch(message)
		return Finding{Line: line, Path: paths.container[line], Message: fmt.Sprintf("field %s is declared twice, first on line %s", m[1], m[2])}
	case wrongShapeRe.MatchString(message):
		m := wrongShapeRe.FindStringSubmatch(message)
		return Finding{Line: line, Path: paths.key[line], Message: fmt.Sprintf("expected %s, found %s", schemaShape(m[2]), documentShape(m[1]))}
	default:
		return Finding{Line: line, Path: paths.key[line], Message: "value does not fit the schema here"}
	}
}

// documentShape names what the operator wrote, from the tag the parser gave it.
func documentShape(tag string) string {
	switch strings.TrimPrefix(tag, "!!") {
	case "str":
		return "text"
	case "int", "float":
		return "a number"
	case "bool":
		return "true or false"
	case "seq":
		return "a list"
	case "map":
		return "a block"
	case "null":
		return "nothing"
	default:
		return "a value of another kind"
	}
}

// schemaShape names what the schema wanted there, from the Go type the decoder
// was aiming at. The type name itself never leaves this function.
func schemaShape(goType string) string {
	switch {
	case strings.HasPrefix(goType, "[]"):
		return "a list"
	case goType == "string":
		return "text"
	case strings.HasPrefix(goType, "int"), strings.HasPrefix(goType, "uint"), strings.HasPrefix(goType, "float"):
		return "a number"
	case goType == "bool":
		return "true or false"
	default:
		return "a block"
	}
}

// pathIndex answers, for a line of the document, which block that line's key
// sits in and what its own structural path is — the two things a decoder
// message positioned by line alone cannot say.
type pathIndex struct {
	// container holds the path of the block the line's key is declared in.
	container map[int]string
	// key holds the path of the line's key itself.
	key map[int]string
}

// indexPaths walks the parsed document recording a structural path for every
// key in it. The first key on a line wins, which is every key outside flow
// style.
func indexPaths(doc *yaml.Node) pathIndex {
	paths := pathIndex{container: map[int]string{}, key: map[int]string{}}
	paths.walk(doc, "")
	return paths
}

// walk records the paths of every key under node, which sits at path.
func (p pathIndex) walk(node *yaml.Node, path string) {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			p.walk(child, path)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			keyPath := key.Value
			if path != "" {
				keyPath = path + "." + key.Value
			}
			if _, seen := p.key[key.Line]; !seen {
				p.key[key.Line] = keyPath
				p.container[key.Line] = path
			}
			p.walk(value, keyPath)
		}
	case yaml.SequenceNode:
		for i, item := range node.Content {
			p.walk(item, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

// atoi reads a line number the decoder wrote, which is always a number; a
// message shaped otherwise never reaches here.
func atoi(digits string) int {
	line := 0
	for _, d := range digits {
		if d < '0' || d > '9' {
			return 0
		}
		line = line*10 + int(d-'0')
	}
	return line
}
