package repofile

import (
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/blueprint"
)

// limitsKey is the repo file key the loop's limits are set under.
const limitsKey = "limits"

// Limits bound the runs of the loop on this repo. A field left zero is unset.
type Limits struct {
	// AgentRuns is the most agent runs one loop run makes. Zero is no cap.
	AgentRuns int
}

// limitsOf reads the limits from node, the value written under limitsKey, as
// limitsFindings has accepted it.
func limitsOf(node *yaml.Node) Limits {
	var l Limits
	for _, e := range effectiveEntries(node) {
		if e.key.Value == "agent_runs" {
			l.AgentRuns, _ = strconv.Atoi(e.value.Value)
		}
	}
	return l
}

// limitsFindings checks that the limits the document sets, if any, are a
// mapping whose only key is agent_runs, a whole number of at least 1,
// reporting each fault with its line. An empty block sets nothing.
func limitsFindings(data []byte) []blueprint.Finding {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	key, value := effectiveEntry(doc.Content[0], limitsKey)
	if key == nil || isEmpty(value) {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return []blueprint.Finding{{Line: key.Line, Path: limitsKey, Message: "limits is a mapping, such as agent_runs: 20"}}
	}
	var findings []blueprint.Finding
	for _, e := range effectiveEntries(value) {
		switch e.key.Value {
		case "agent_runs":
			if n, err := strconv.Atoi(e.value.Value); e.value.Kind != yaml.ScalarNode || e.value.Tag != "!!int" || err != nil || n < 1 {
				findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: limitsKey + ".agent_runs", Message: "agent_runs must be a whole number of at least 1"})
			}
		default:
			findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: limitsKey, Message: `unknown field "` + e.key.Value + `"`})
		}
	}
	return findings
}
