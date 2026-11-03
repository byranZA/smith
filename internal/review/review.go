// Package review holds what a review is made of: the findings file its agent
// writes, and the comment the loop posts about it. It is pure: plain inputs
// and outputs, no I/O.
package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// The axes a finding is made on, in the order they are filed, with how each
// is written in a findings file, named in a comment and numbered in an id.
var axes = []struct{ key, name, prefix string }{
	{"spec", "Spec", "S"},
	{"standards", "Standards", "St"},
	{"quality", "Quality", "Q"},
}

// severities are the severities a finding may carry, worst first.
var severities = []string{"blocker", "major", "minor"}

// Parse reads and validates data as a findings file, refusing the whole file
// on any error, with a message the attempt's report can show.
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
	for i, f := range *raw.Findings {
		if err := f.validate(len(*raw.Findings)); err != nil {
			return File{}, fmt.Errorf("findings[%d] %w", i, err)
		}
	}
	if err := noCycle(*raw.Findings); err != nil {
		return File{}, err
	}
	return File{Findings: *raw.Findings, NotFiled: raw.NotFiled}, nil
}

// validate reports what is wrong with f, one of count findings, or nil.
func (f Finding) validate(count int) error {
	if axisIndex(f.Axis) < 0 {
		return fmt.Errorf("has axis %q, want spec, standards or quality", f.Axis)
	}
	if !slices.Contains(severities, f.Severity) {
		return fmt.Errorf("has severity %q, want blocker, major or minor", f.Severity)
	}
	if f.Who != "agent" && f.Who != "human" {
		return fmt.Errorf("has who %q, want agent or human", f.Who)
	}
	if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" {
		return errors.New("needs a title and a body")
	}
	for _, b := range f.BlockedBy {
		if b < 1 || b > count {
			return fmt.Errorf("is blocked by %d, which is not a position in findings (1 to %d)", b, count)
		}
	}
	return nil
}

// noCycle refuses findings whose blocked_by lists lead back to themselves,
// naming a finding itself as its own blocker among them.
func noCycle(findings []Finding) error {
	const (
		visiting = 1
		done     = 2
	)
	state := make([]int, len(findings))
	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			return fmt.Errorf("findings[%d] is blocked by itself, directly or through other findings", i)
		case done:
			return nil
		}
		state[i] = visiting
		for _, b := range findings[i].BlockedBy {
			if err := visit(b - 1); err != nil {
				return err
			}
		}
		state[i] = done
		return nil
	}
	for i := range findings {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

// axisIndex is the place of axis key in the filing order, or -1.
func axisIndex(key string) int {
	return slices.IndexFunc(axes, func(a struct{ key, name, prefix string }) bool { return a.key == key })
}

// IDs returns the id of each finding, from file order and counted per axis:
// S1, S2, St1, Q1.
func IDs(findings []Finding) []string {
	next := make([]int, len(axes))
	ids := make([]string, len(findings))
	for i, f := range findings {
		a := axisIndex(f.Axis)
		next[a]++
		ids[i] = fmt.Sprintf("%s%d", axes[a].prefix, next[a])
	}
	return ids
}

// Order returns the positions of findings in the order they are filed: Spec,
// then Standards, then Quality, each in file order, with a finding always
// after those it is blocked by.
func Order(findings []Finding) []int {
	byAxis := make([]int, 0, len(findings))
	for a := range axes {
		for i, f := range findings {
			if axisIndex(f.Axis) == a {
				byAxis = append(byAxis, i)
			}
		}
	}
	placed := make([]bool, len(findings))
	order := make([]int, 0, len(findings))
	var place func(i int)
	place = func(i int) {
		if placed[i] {
			return
		}
		placed[i] = true
		for _, b := range findings[i].BlockedBy {
			place(b - 1)
		}
		order = append(order, i)
	}
	for _, i := range byAxis {
		place(i)
	}
	return order
}

// TaskBody renders the body of the task that files f, whose id is id, from the
// review of issue number: its body and a Source line.
func TaskBody(f Finding, id string, number int) string {
	return fmt.Sprintf("%s\n\nSource: %s axis, %s, finding %s, review of #%d\n", strings.TrimSpace(f.Body), axes[axisIndex(f.Axis)].name, f.Severity, id, number)
}

// ledger is what the hidden block of a review comment holds.
type ledger struct {
	Sha      string    `json:"sha"`
	Findings []Finding `json:"findings"`
}

// Comment renders the review of issue number as the comment the loop posts:
// a section for each axis, the restructurings not filed, a tally with the
// worst finding, and a hidden block holding the findings and the work branch
// sha reviewed. tasks maps the position of each finding already filed to the
// number of its task, which the comment names; nil names none.
func Comment(file File, number int, sha string, tasks map[int]int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "smith review of #%d\n\n", number)
	ids := IDs(file.Findings)
	counts := make([]string, len(axes))
	for a, axis := range axes {
		fmt.Fprintf(&b, "## %s\n\n", axis.name)
		n := 0
		for i, f := range file.Findings {
			if f.Axis != axis.key {
				continue
			}
			fmt.Fprintf(&b, "- **%s**: %s", f.Severity, f.Title)
			if task, ok := tasks[i]; ok {
				fmt.Fprintf(&b, " (#%d)", task)
			}
			b.WriteString("\n")
			n++
		}
		if n == 0 {
			b.WriteString("No findings.\n")
		}
		counts[a] = fmt.Sprintf("%d %s", n, axis.name)
		b.WriteString("\n")
	}
	if len(file.NotFiled) > 0 {
		b.WriteString("**Not filed**\n\n")
		for _, n := range file.NotFiled {
			fmt.Fprintf(&b, "- %s: %s\n", n.Title, n.Summary)
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(counts, ", "))
	if worst := worstOf(file.Findings); worst >= 0 {
		f := file.Findings[worst]
		fmt.Fprintf(&b, ". Worst: **%s** %s (%s)", f.Severity, f.Title, ids[worst])
	}
	b.WriteString(".\n\n")
	// json.Marshal escapes ">", so the block cannot end the comment early.
	block, err := json.Marshal(ledger{Sha: sha, Findings: file.Findings})
	if err != nil {
		panic(fmt.Sprintf("marshal the review ledger: %v", err))
	}
	fmt.Fprintf(&b, "<!-- smith:review %s -->\n", block)
	return b.String()
}

// worstOf is the position of the worst finding, the first of them in filing
// order, or -1 when there are none.
func worstOf(findings []Finding) int {
	worst, rank := -1, len(severities)
	for _, i := range Order(findings) {
		if r := slices.Index(severities, findings[i].Severity); r < rank {
			worst, rank = i, r
		}
	}
	return worst
}
