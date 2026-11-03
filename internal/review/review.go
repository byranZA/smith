// Package review holds what a review is made of: the findings file its agent
// writes, and the comment the loop posts about it. It is pure: plain inputs
// and outputs, no I/O.
package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Finding is one thing the review found wrong with the work.
type Finding struct {
	Axis      string `json:"axis"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Who       string `json:"who"`
	Body      string `json:"body"`
	BlockedBy []int  `json:"blocked_by,omitempty"`
}

// NotFiled is a restructuring too large for a task, reported but never filed.
type NotFiled struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// File is a findings file as the review agent wrote it.
type File struct {
	Findings []Finding  `json:"findings"`
	NotFiled []NotFiled `json:"not_filed,omitempty"`
}

// Parse reads and validates data as a findings file, refusing the whole file
// on any error, with a message the attempt's report can show. The loop does
// not file findings yet, so a file holding any is refused.
func Parse(data []byte) (File, error) {
	var raw struct {
		Findings *[]Finding `json:"findings"`
		NotFiled []NotFiled `json:"not_filed"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return File{}, fmt.Errorf("the findings file is not valid: %w", err)
	}
	if raw.Findings == nil {
		return File{}, errors.New(`the findings file has no "findings" list`)
	}
	for i, n := range raw.NotFiled {
		if strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Summary) == "" {
			return File{}, fmt.Errorf("not_filed[%d] needs a title and a summary", i)
		}
	}
	if len(*raw.Findings) > 0 {
		return File{}, fmt.Errorf("the findings file holds %d findings, which the loop cannot file yet", len(*raw.Findings))
	}
	return File{Findings: *raw.Findings, NotFiled: raw.NotFiled}, nil
}

// ledger is what the hidden block of a review comment holds.
type ledger struct {
	Sha      string    `json:"sha"`
	Findings []Finding `json:"findings"`
}

// Comment renders the review of issue number as the comment the loop posts:
// a section for each axis, the restructurings not filed, and a hidden block
// holding the findings and the work branch sha reviewed.
func Comment(file File, number int, sha string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "smith review of #%d\n\n", number)
	for _, axis := range []struct{ name, key string }{{"Spec", "spec"}, {"Standards", "standards"}, {"Quality", "quality"}} {
		fmt.Fprintf(&b, "## %s\n\n", axis.name)
		listed := false
		for _, f := range file.Findings {
			if f.Axis == axis.key {
				fmt.Fprintf(&b, "- **%s**: %s\n", f.Severity, f.Title)
				listed = true
			}
		}
		if !listed {
			b.WriteString("No findings.\n")
		}
		b.WriteString("\n")
	}
	if len(file.NotFiled) > 0 {
		b.WriteString("**Not filed**\n\n")
		for _, n := range file.NotFiled {
			fmt.Fprintf(&b, "- %s: %s\n", n.Title, n.Summary)
		}
		b.WriteString("\n")
	}
	block, err := json.Marshal(ledger{Sha: sha, Findings: file.Findings})
	if err != nil {
		panic(fmt.Sprintf("marshal the review ledger: %v", err))
	}
	// A "--" cannot end the comment early, so escape the hyphen in the JSON.
	fmt.Fprintf(&b, "<!-- smith:review %s -->\n", strings.ReplaceAll(string(block), "--", `--`))
	return b.String()
}
