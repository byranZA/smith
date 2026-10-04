package agent

import (
	"fmt"
	"slices"
	"strings"
)

// Effort is smith's own scale for how hard an agent thinks, which each adapter
// translates into its CLI's terms. The empty Effort is unset.
type Effort string

// The efforts on smith's scale, least first.
const (
	Low    Effort = "low"
	Medium Effort = "medium"
	High   Effort = "high"
)

// scale is every effort smith knows, least first.
var scale = []Effort{Low, Medium, High}

// EffortError reports an effort that is not on smith's scale.
type EffortError struct {
	Effort string
}

// Error implements error.
func (e *EffortError) Error() string {
	names := make([]string, len(scale))
	for i, effort := range scale {
		names[i] = string(effort)
	}
	return fmt.Sprintf("unknown effort %q: the scale is %s", e.Effort, strings.Join(names, ", "))
}

// ParseEffort reads s as an effort on smith's scale. The empty string is the
// unset Effort; anything else off the scale is an *EffortError.
func ParseEffort(s string) (Effort, error) {
	if s == "" || slices.Contains(scale, Effort(s)) {
		return Effort(s), nil
	}
	return "", &EffortError{Effort: s}
}
