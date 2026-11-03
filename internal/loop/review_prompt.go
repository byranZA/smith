package loop

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed review.md
var builtinReviewPrompt string

// ReviewPrompt is the review prompt: the text handed to the review agent,
// with {{ISSUE_NUMBER}}, {{ISSUE_TITLE}}, {{MERGE_BASE}} and {{FINDINGS_FILE}}
// placeholders.
type ReviewPrompt string

// BuiltinReviewPrompt returns the review prompt smith ships, placeholders
// unfilled.
func BuiltinReviewPrompt() ReviewPrompt { return ReviewPrompt(builtinReviewPrompt) }

// ReviewTarget is what a review prompt is filled in with.
type ReviewTarget struct {
	// Number and Title name the issue whose work is reviewed.
	Number int
	Title  string
	// MergeBase is the commit the work branch left its base at.
	MergeBase string
	// FindingsFile is the absolute path the agent writes its findings to.
	FindingsFile string
}

// Render fills p's placeholders in with target.
func (p ReviewPrompt) Render(target ReviewTarget) string {
	return strings.NewReplacer(
		"{{ISSUE_NUMBER}}", strconv.Itoa(target.Number),
		"{{ISSUE_TITLE}}", target.Title,
		"{{MERGE_BASE}}", target.MergeBase,
		"{{FINDINGS_FILE}}", target.FindingsFile,
	).Replace(string(p))
}
