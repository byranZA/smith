package repofile

import (
	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/blueprint"
)

// stackKey is the repo file key the stack is set under.
const stackKey = "stack"

// Stack is the pair of commands that bring a repo's services up and take them
// down. Both empty is no stack.
type Stack struct {
	// Up brings the services up, run by sh -c in the worktree under its
	// toolchain. It must be safe to re-run and not return before the stack is
	// ready.
	Up string
	// Down takes down what Up started.
	Down string
}

// Declared reports whether the repo file declares a stack.
func (s Stack) Declared() bool { return s != Stack{} }

// stackOf reads the stack from node, the value written under stackKey, as
// stackFindings has accepted it.
func stackOf(node *yaml.Node) Stack {
	var s Stack
	for _, e := range effectiveEntries(node) {
		switch e.key.Value {
		case "up":
			s.Up = e.value.Value
		case "down":
			s.Down = e.value.Value
		}
	}
	return s
}

// stackFindings checks that the stack the document sets, if any, is a mapping
// of one non-empty string up and one non-empty string down and nothing else,
// reporting each fault with its line.
func stackFindings(data []byte) []blueprint.Finding {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	key, value := effectiveEntry(doc.Content[0], stackKey)
	if key == nil {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return []blueprint.Finding{{Line: key.Line, Path: stackKey, Message: "a stack is a mapping with an up and a down command"}}
	}
	var findings []blueprint.Finding
	seen := map[string]bool{}
	for _, e := range effectiveEntries(value) {
		name := e.key.Value
		switch name {
		case "up", "down":
			seen[name] = true
			if e.value.Kind != yaml.ScalarNode || e.value.Tag != "!!str" {
				findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: stackKey + "." + name, Message: name + " must be one string"})
			} else if e.value.Value == "" {
				findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: stackKey + "." + name, Message: name + " must be a non-empty string"})
			}
		default:
			findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: stackKey, Message: `unknown field "` + name + `"`})
		}
	}
	if !seen["up"] {
		findings = append(findings, blueprint.Finding{Line: key.Line, Path: stackKey, Message: "up is required: a stack must say how to start"})
	}
	if !seen["down"] {
		findings = append(findings, blueprint.Finding{Line: key.Line, Path: stackKey, Message: "down is required: a stack must say how to stop what it starts"})
	}
	return findings
}
