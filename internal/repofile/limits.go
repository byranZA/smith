package repofile

import (
	"fmt"
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
	// TaskAttempts is the most agent runs each task, and the review, gets
	// before it is skipped. Zero is unset.
	TaskAttempts int
}

// limitsOf reads the limits from node, the value written under limitsKey, as
// limitsFindings has accepted it.
func limitsOf(node *yaml.Node) (Limits, error) {
	var l Limits
	for _, e := range effectiveEntries(node) {
		var dst *int
		switch e.key.Value {
		case "agent_runs":
			dst = &l.AgentRuns
		case "task_attempts":
			dst = &l.TaskAttempts
		default:
			continue
		}
		n, err := strconv.Atoi(e.value.Value)
		if err != nil {
			return Limits{}, fmt.Errorf("read limits.%s: %w", e.key.Value, err)
		}
		*dst = n
	}
	return l, nil
}

// limitsFindings checks that the limits the document sets, if any, are a
// mapping whose only keys are agent_runs and task_attempts, each a whole number of at least 1,
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
		case "agent_runs", "task_attempts":
			if n, err := strconv.Atoi(e.value.Value); e.value.Kind != yaml.ScalarNode || e.value.Tag != "!!int" || err != nil || n < 1 {
				findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: limitsKey + "." + e.key.Value, Message: e.key.Value + " must be a whole number of at least 1"})
			}
		default:
			findings = append(findings, blueprint.Finding{Line: e.key.Line, Path: limitsKey, Message: `unknown field "` + e.key.Value + `"`})
		}
	}
	return findings
}
