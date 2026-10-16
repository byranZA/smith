package loop

import (
	"context"
	"fmt"
	"slices"

	"github.com/byranZA/smith/internal/config"
)

// LabelLister reads which labels the repo the loop works has.
type LabelLister interface {
	// Labels returns the names of the repo's labels.
	Labels(ctx context.Context) ([]string, error)
}

// LabelWarnings returns one warning for each triage label the repo file sets
// that repo lacks, saying what that means, or a single warning when the
// labels cannot be read. A label left to its default is never checked, and
// with none set repo is not read at all.
func LabelWarnings(ctx context.Context, repo LabelLister, s Settings) []string {
	var set []namedLabel
	for _, l := range s.namedLabels() {
		if l.label.Origin == config.FromRepoFile {
			set = append(set, l)
		}
	}
	if len(set) == 0 {
		return nil
	}
	have, err := repo.Labels(ctx)
	if err != nil {
		return []string{fmt.Sprintf("could not check the repo has the labels the repo file sets: %v", err)}
	}
	var warnings []string
	for _, l := range set {
		if !slices.Contains(have, l.label.Value) {
			warnings = append(warnings, fmt.Sprintf("%s is %q, a label this repo does not have: %s", l.key, l.label.Value, missingMeans(l.key)))
		}
	}
	return warnings
}

// missingMeans says what a triage label the repo lacks means for the run, by
// the repo file key that sets it: only the human label is ever written, so
// only it is created when first needed.
func missingMeans(key string) string {
	if key == humanKey {
		return "smith will create it the first time it skips a task"
	}
	return "no issue will match it, and smith never creates it"
}
