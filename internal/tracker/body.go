package tracker

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// issueRef matches a reference to an issue in a body, as #43, owner/name#43
// or an issue URL, capturing the owner/name a qualified one names.
var issueRef = regexp.MustCompile(`(?:[^/\s]+/([\w.-]+/[\w.-]+)/issues/|([\w.-]+/[\w.-]+)#|#)(\d+)\b`)

// heading matches a Markdown heading line and captures its text.
var heading = regexp.MustCompile(`^\s*#{1,6}\s+(.*?)\s*$`)

// listedTasks returns the issues a spec body's "## Tasks" checklist
// names, in the order it names them, each once.
func listedTasks(body string) []Ref {
	return refsIn(section(body, "Tasks"))
}

// blockersIn returns the issues a task body's "## Blocked by" section
// names, each once. A section that says "None" names none.
func blockersIn(body string) []Ref {
	_, header := fallbackLinks(body)
	blockers := slices.Clone(header)
	for _, ref := range refsIn(section(body, "Blocked by")) {
		if !slices.Contains(blockers, ref) {
			blockers = append(blockers, ref)
		}
	}
	return blockers
}

// fallbackLink matches a line of the head of a body that a gh without the link
// flags was given in their place, capturing which link it is and its issue.
var fallbackLink = regexp.MustCompile(`^(Part of|Blocked by) #(\d+)\s*$`)

// fallbackLinks reads the "Part of #N" and "Blocked by #N" lines that head a
// body, up to its first other line: the issue it is part of, or 0 when it is
// part of none, and the issues blocking it.
func fallbackLinks(body string) (parent int, blockers []Ref) {
	for line := range strings.SplitSeq(body, "\n") {
		m := fallbackLink.FindStringSubmatch(line)
		if m == nil {
			break
		}
		n, err := strconv.Atoi(m[2])
		switch {
		case err != nil:
		case m[1] == "Part of":
			parent = n
		default:
			blockers = append(blockers, Ref{Number: n})
		}
	}
	return parent, blockers
}

// section returns the lines under the first heading titled name, matched
// without regard to case or level, up to the next heading.
func section(body, name string) string {
	var lines []string
	inside := false
	for line := range strings.SplitSeq(body, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			if inside {
				break
			}
			inside = strings.EqualFold(m[1], name)
			continue
		}
		if inside {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// refsIn returns the issues referenced in text, in order, each once, keeping
// the repo a qualified reference names.
func refsIn(text string) []Ref {
	var refs []Ref
	seen := map[Ref]bool{}
	for _, m := range issueRef.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[3])
		ref := Ref{Repo: m[1] + m[2], Number: n}
		if err != nil || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}
