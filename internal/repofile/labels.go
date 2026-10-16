package repofile

import (
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/blueprint"
)

// labelsKey is the repo file key the triage labels are set under.
const labelsKey = "labels"

// labelFindings checks each label the document sets under labelsKey names a
// label, reporting each one that does not with its line. A document the
// strict decoder accepted is always YAML, so data parses here.
func labelFindings(data []byte) []blueprint.Finding {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	labels := mappingValue(doc.Content[0], labelsKey)
	if labels == nil {
		return nil
	}
	var report []blueprint.Finding
	claimed := map[string]*yaml.Node{}
	for i := 0; i+1 < len(labels.Content); i += 2 {
		key, value := labels.Content[i], resolved(labels.Content[i+1])
		path := labelsKey + "." + key.Value
		first, taken := claimed[value.Value]
		switch {
		case isEmpty(value):
			report = append(report, blueprint.Finding{Line: key.Line, Path: path, Message: "a label needs a name: set one, or leave the key out for the default"})
		case taken:
			report = append(report, blueprint.Finding{
				Line:    key.Line,
				Path:    path,
				Message: fmt.Sprintf("label %q is already %s.%s, on line %d: each label needs a name of its own", value.Value, labelsKey, first.Value, first.Line),
			})
		default:
			claimed[value.Value] = key
		}
	}
	return report
}

// mappingValue returns the mapping under key in node, or nil when node is no
// mapping or holds no mapping under key.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if value := resolved(node.Content[i+1]); node.Content[i].Value == key && value.Kind == yaml.MappingNode {
			return value
		}
	}
	return nil
}

// resolved returns the node an alias stands for, following aliases of
// aliases, and any other node as it is.
func resolved(node *yaml.Node) *yaml.Node {
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}

// isEmpty reports whether value is a scalar naming nothing: empty text, or null.
func isEmpty(value *yaml.Node) bool {
	return value.Kind == yaml.ScalarNode && (value.Value == "" || value.Tag == "!!null")
}

// validationError returns the findings as the repo file's *blueprint.ValidationError,
// or nil when there are none.
func validationError(findings []blueprint.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	return fmt.Errorf("parse repo file: %w", &blueprint.ValidationError{Subject: subject, Findings: findings})
}
