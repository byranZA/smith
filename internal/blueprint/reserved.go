package blueprint

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// reservedFields are the schema's own field names, which mean nothing as a
// tool or an environment variable and everything as a misindented line. The
// match is deliberately case-sensitive: every schema field is lowercase, so an
// environment variable named URL or MODE is untouched and only the misindent
// shape trips.
var reservedFields = map[string]bool{
	"access": true, "terminal": true, "workspace": true, "provider": true,
	"packages": true, "tools": true, "env": true, "placements": true,
	"repos": true, "git": true, "name": true, "url": true, "base": true,
	"from": true, "to": true, "mode": true, "perms": true,
}

// openMaps are the two blocks whose keys the operator invents, and so the two
// the strict pass cannot police: a schema field landing in either parses
// perfectly and stands up a box missing the field it was meant to set.
var openMaps = []string{"tools", "env"}

// reservedFindings reports a schema field name used as a key inside tools or
// env, at box scope or repo scope, in the order the keys appear in the
// document. It reads the parsed document rather than the decoded blueprint
// because the operator needs the line, and a map has lost it.
func reservedFindings(doc *yaml.Node) []Finding {
	root := mappingOf(doc)
	if root == nil {
		return nil
	}
	report := openMapFindings(root, "")
	for i, repo := range sequenceUnder(root, "repos") {
		report = append(report, openMapFindings(mappingOf(repo), fmt.Sprintf("repos[%d]", i))...)
	}
	return report
}

// openMapFindings reports the reserved keys inside the open maps of one scope,
// which sits at path.
func openMapFindings(scope *yaml.Node, path string) []Finding {
	var report []Finding
	for _, name := range openMaps {
		at := name
		if path != "" {
			at = path + "." + name
		}
		open := mappingOf(valueUnder(scope, name))
		if open == nil {
			continue
		}
		for i := 0; i+1 < len(open.Content); i += 2 {
			key := open.Content[i]
			if !reservedFields[key.Value] {
				continue
			}
			report = append(report, Finding{
				Line: key.Line,
				Path: at,
				Message: fmt.Sprintf(
					"%q is a schema field name and is reserved inside %q: check the indentation, a field written one level too deep lands here as an entry smith would never use",
					key.Value, name),
			})
		}
	}
	return report
}

// mappingOf is the block node itself, following a document wrapper down to the
// block it holds. It is nil for anything that is not a block.
func mappingOf(node *yaml.Node) *yaml.Node {
	switch {
	case node == nil:
		return nil
	case node.Kind == yaml.DocumentNode && len(node.Content) == 1:
		return mappingOf(node.Content[0])
	case node.Kind == yaml.MappingNode:
		return node
	default:
		return nil
	}
}

// valueUnder is the value declared for key in block, or nil when the block
// does not declare it.
func valueUnder(block *yaml.Node, key string) *yaml.Node {
	if block == nil {
		return nil
	}
	for i := 0; i+1 < len(block.Content); i += 2 {
		if block.Content[i].Value == key {
			return block.Content[i+1]
		}
	}
	return nil
}

// sequenceUnder is the list declared for key in block, empty when the block
// declares no such list.
func sequenceUnder(block *yaml.Node, key string) []*yaml.Node {
	value := valueUnder(block, key)
	if value == nil || value.Kind != yaml.SequenceNode {
		return nil
	}
	return value.Content
}
