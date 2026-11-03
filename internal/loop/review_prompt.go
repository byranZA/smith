package loop

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
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

// reviewPlaceholders are the placeholders a review prompt may hold, in the
// order a refusal lists them.
var reviewPlaceholders = []string{"{{ISSUE_NUMBER}}", "{{ISSUE_TITLE}}", "{{MERGE_BASE}}", "{{FINDINGS_FILE}}"}

// LoadReviewPrompt reads the review prompt a repo ejected to path, and reports
// whether it found one. With none it returns the built-in prompt. It refuses a
// placeholder smith does not fill.
func LoadReviewPrompt(path string) (prompt ReviewPrompt, ejected bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return BuiltinReviewPrompt(), false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read review prompt: %w", err)
	}
	prompt, err = ParseReviewPrompt(path, data)
	return prompt, err == nil, err
}

// ParseReviewPrompt takes data as an ejected review prompt read from source,
// refusing a placeholder smith does not fill and naming source.
func ParseReviewPrompt(source string, data []byte) (ReviewPrompt, error) {
	for _, found := range placeholderPattern.FindAllString(string(data), -1) {
		if !slices.Contains(reviewPlaceholders, found) {
			return "", fmt.Errorf("%s: unknown placeholder %s: known placeholders are %s", source, found, strings.Join(reviewPlaceholders, ", "))
		}
	}
	return ReviewPrompt(data), nil
}

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
