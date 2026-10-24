package repofile

import (
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/blueprint"
)

// prepareKey is the repo file key the prepare command is set under.
const prepareKey = "prepare"

// prepareFindings checks that the prepare command the document sets, if any,
// is one non-empty string, reporting it with its key's line when it is not. A
// document the strict decoder accepted is always YAML, so data parses here.
func prepareFindings(data []byte) []blueprint.Finding {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	key, value := effectiveEntry(doc.Content[0], prepareKey)
	if key == nil || isCommand(value) {
		return nil
	}
	return []blueprint.Finding{{Line: key.Line, Path: prepareKey, Message: `a prepare command is one non-empty string, such as "uv sync": set one, or leave the key out to run none`}}
}

// entry is one key a mapping sets and the resolved value it sets it to.
type entry struct {
	key, value *yaml.Node
}

// effectiveEntry returns the key and resolved value that set name in node, as
// effectiveEntries finds it. Both are nil when node is no mapping or nothing
// in it sets name.
func effectiveEntry(node *yaml.Node, name string) (key, value *yaml.Node) {
	for _, e := range effectiveEntries(node) {
		if e.key.Value == name {
			return e.key, e.value
		}
	}
	return nil, nil
}

// effectiveEntries returns each key node sets and the entry that sets it,
// following merge keys with the decoder's precedence: a mapping's own keys
// over the maps it merges, and an earlier merged map over a later one. A
// mapping's own entries come first, in document order, then those it merges.
// It is empty when node is no mapping.
func effectiveEntries(node *yaml.Node) []entry {
	node = resolved(node)
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var entries []entry
	var merge *yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		switch k := node.Content[i]; {
		case k.ShortTag() == "!!merge":
			merge = resolved(node.Content[i+1])
		default:
			entries = append(entries, entry{k, resolved(node.Content[i+1])})
		}
	}
	if merge == nil {
		return entries
	}
	merged := []*yaml.Node{merge}
	if merge.Kind == yaml.SequenceNode {
		merged = merge.Content
	}
	for _, m := range merged {
		for _, e := range effectiveEntries(m) {
			if !slices.ContainsFunc(entries, func(set entry) bool { return set.key.Value == e.key.Value }) {
				entries = append(entries, e)
			}
		}
	}
	return entries
}

// isCommand reports whether value is a command: a string with text in it.
func isCommand(value *yaml.Node) bool {
	return value.Kind == yaml.ScalarNode && value.Tag == "!!str" && value.Value != ""
}
