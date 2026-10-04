package tracker

import (
	"regexp"
	"strconv"
	"strings"
)

// issueRef matches a reference to an issue in a body, as #43 or as an issue URL.
var issueRef = regexp.MustCompile(`(?:#|/issues/)(\d+)\b`)

// heading matches a Markdown heading line and captures its text.
var heading = regexp.MustCompile(`^\s*#{1,6}\s+(.*?)\s*$`)

// listedTasks returns the issue numbers a spec body's "## Tasks" checklist
// names, in the order it names them, each once.
func listedTasks(body string) []int {
	return refsIn(section(body, "Tasks"))
}

// blockersIn returns the issue numbers a task body's "## Blocked by" section
// names, each once. A section that says "None" names none.
func blockersIn(body string) []int {
	return refsIn(section(body, "Blocked by"))
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

// refsIn returns the issue numbers referenced in text, in order, each once.
func refsIn(text string) []int {
	var refs []int
	seen := map[int]bool{}
	for _, m := range issueRef.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		refs = append(refs, n)
	}
	return refs
}
