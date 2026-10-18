package repofile

import (
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
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], resolved(root.Content[i+1])
		if key.Value != prepareKey || isCommand(value) {
			continue
		}
		return []blueprint.Finding{{Line: key.Line, Path: prepareKey, Message: `a prepare command is one non-empty string, such as "uv sync": set one, or leave the key out to run none`}}
	}
	return nil
}

// isCommand reports whether value is a command: a string with text in it.
func isCommand(value *yaml.Node) bool {
	return value.Kind == yaml.ScalarNode && value.Tag == "!!str" && value.Value != ""
}
