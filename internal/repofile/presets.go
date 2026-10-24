package repofile

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/blueprint"
)

// The repo file keys presets and roles are set under.
const (
	presetsKey = "presets"
	rolesKey   = "roles"
)

// WorkRole is the role that works a spec's tasks, attached or not. It is the
// only role smith defines.
const WorkRole = "work"

// roles is every role smith defines.
var roles = []string{WorkRole}

// presetName is the shape of a preset's name, which a task label carries.
var presetName = regexp.MustCompile(`^[a-z0-9-]+$`)

// flatKeys are the retired top-level keys a preset now holds.
var flatKeys = []string{"agent", "model", "effort"}

// flatFindings reports each retired flat agent, model and effort key the
// document sets, with its line and the presets shape that replaces it.
func flatFindings(data []byte) []blueprint.Finding {
	top := root(data)
	if top == nil {
		return nil
	}
	var report []blueprint.Finding
	for _, name := range flatKeys {
		key, _ := effectiveEntry(top, name)
		if key == nil {
			continue
		}
		report = append(report, blueprint.Finding{
			Line: key.Line,
			Path: name,
			Message: name + " is set in a preset now, not at the top level: name the agent, model and effort as a preset and point the work role at it," +
				" as in presets: {opus: {agent: claude, model: opus, effort: high}} and roles: {work: opus}",
		})
	}
	return report
}

// presetFindings checks every preset the document defines, used or not: its
// name is lowercase letters, digits and hyphens, its agent is one smith has an
// adapter for, and its effort is on smith's scale. Each one that is not is
// reported with its line.
func presetFindings(data []byte) []blueprint.Finding {
	top := root(data)
	if top == nil {
		return nil
	}
	presets := mappingValue(top, presetsKey)
	if presets == nil {
		return nil
	}
	var report []blueprint.Finding
	for i := 0; i+1 < len(presets.Content); i += 2 {
		name, preset := presets.Content[i], resolved(presets.Content[i+1])
		path := presetsKey + "." + name.Value
		if !presetName.MatchString(name.Value) {
			report = append(report, blueprint.Finding{Line: name.Line, Path: path, Message: fmt.Sprintf("preset name %q may hold only lowercase letters, digits and hyphens", name.Value)})
		}
		switch key, value := effectiveEntry(preset, "agent"); {
		case key == nil || isEmpty(value):
			report = append(report, blueprint.Finding{Line: name.Line, Path: path, Message: "a preset needs an agent: one of " + strings.Join(agent.Known(), ", ")})
		default:
			if _, err := agent.Lookup(value.Value); err != nil {
				report = append(report, blueprint.Finding{Line: key.Line, Path: path + ".agent", Message: err.Error()})
			}
		}
		if key, value := effectiveEntry(preset, "effort"); key != nil {
			if _, err := agent.ParseEffort(value.Value); err != nil {
				report = append(report, blueprint.Finding{Line: key.Line, Path: path + ".effort", Message: err.Error()})
			}
		}
	}
	return report
}

// roleFindings checks each role the document sets is one smith defines and
// names a preset in presets, reporting each one that is not with its line.
func roleFindings(data []byte, presets map[string]Preset) []blueprint.Finding {
	top := root(data)
	if top == nil {
		return nil
	}
	set := mappingValue(top, rolesKey)
	if set == nil {
		return nil
	}
	var report []blueprint.Finding
	for i := 0; i+1 < len(set.Content); i += 2 {
		role, value := set.Content[i], resolved(set.Content[i+1])
		path := rolesKey + "." + role.Value
		if !slices.Contains(roles, role.Value) {
			report = append(report, blueprint.Finding{Line: role.Line, Path: path, Message: fmt.Sprintf("smith has no role %q: the only role is %s", role.Value, strings.Join(roles, ", "))})
			continue
		}
		if _, ok := presets[value.Value]; !ok {
			report = append(report, blueprint.Finding{Line: role.Line, Path: path, Message: fmt.Sprintf("role %s names preset %q, which %s does not define", role.Value, value.Value, presetsKey)})
		}
	}
	return report
}

// root returns the document's top-level node, or nil when data holds none. A
// document the strict decoder accepted is always YAML, so data parses here.
func root(data []byte) *yaml.Node {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}
