package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// The labels that say what an issue is for on GitHub.
const (
	specLabel  = "spec"
	agentLabel = "ready-for-agent"
	humanLabel = "ready-for-human"
)

// The issue fields read for a spec and for a task.
const (
	specFields = "number,title,state,labels,body,subIssues"
	taskFields = "number,title,state,labels,body,blockedBy"
)

// linkedQuery reads one page of an issue's linked issues over the connection
// named by its %s verb, in the repo gh resolves for the working directory.
const linkedQuery = `query($owner: String!, $repo: String!, $number: Int!, $endCursor: String) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) {
      %s(first: 100, after: $endCursor) {
        nodes { number state }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

// notFoundMark is how gh says an issue number resolves to nothing.
const notFoundMark = "Could not resolve to an issue"

// Runner launches a command on the operator's machine. It is the boundary gh
// is reached through, injected so a test can stand in for gh.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// GitHub is the tracker over GitHub Issues, read through the gh CLI in the
// repo smith is run from. It only ever reads.
type GitHub struct {
	gh Runner
}

// NewGitHub returns the GitHub tracker reaching gh through runner.
func NewGitHub(runner Runner) GitHub {
	return GitHub{gh: runner}
}

// issue is the part of gh's issue JSON the tracker reads.
type issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Body      string     `json:"body"`
	Labels    []label    `json:"labels"`
	SubIssues connection `json:"subIssues"`
	BlockedBy connection `json:"blockedBy"`
}

// connection is a list of linked issues as gh's issue view gives it: at most
// one page of them, and how many there are in all.
type connection struct {
	Nodes      []issueNode `json:"nodes"`
	TotalCount int         `json:"totalCount"`
}

// linkedPage is GitHub's answer for one page of an issue's linked issues.
type linkedPage struct {
	Data struct {
		Repository struct {
			Issue map[string]struct {
				Nodes    []issueNode `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

// label is a label as gh lists it on an issue.
type label struct {
	Name string `json:"name"`
}

// issueNode is a linked issue as gh lists it under another.
type issueNode struct {
	Number int    `json:"number"`
	State  string `json:"state"`
}

// Spec reads spec number and its tasks: the spec's sub-issues, those its
// "## Tasks" checklist lists first and in that order, then any other child in
// the order GitHub holds them, which takes in a task filed after the spec was
// written. A task's blockers are its blocked-by links and the issues its
// "## Blocked by" section names.
func (g GitHub) Spec(ctx context.Context, number int) (Spec, error) {
	spec, err := g.view(ctx, number, specFields)
	if err != nil {
		return Spec{}, err
	}
	if !spec.labelled(specLabel) {
		return Spec{}, &NotSpecError{Number: number}
	}
	subIssues, err := g.linked(ctx, number, "subIssues", spec.SubIssues)
	if err != nil {
		return Spec{}, err
	}
	children := make([]int, 0, len(subIssues))
	for _, node := range subIssues {
		children = append(children, node.Number)
	}
	issues := map[int]issue{}
	for _, n := range children {
		task, err := g.view(ctx, n, taskFields)
		if err != nil {
			return Spec{}, fmt.Errorf("read task of spec #%d: %w", number, err)
		}
		task.BlockedBy.Nodes, err = g.linked(ctx, n, "blockedBy", task.BlockedBy)
		if err != nil {
			return Spec{}, fmt.Errorf("read task of spec #%d: %w", number, err)
		}
		issues[n] = task
	}
	tasks := make([]Task, 0, len(children))
	for _, n := range listedOrder(listedTasks(spec.Body), children) {
		blockers, err := g.blockers(ctx, issues[n], issues)
		if err != nil {
			return Spec{}, err
		}
		tasks = append(tasks, Task{Number: n, Title: issues[n].Title, Open: isOpen(issues[n].State), For: issues[n].audience(), Blockers: blockers})
	}
	return Spec{Number: spec.Number, Title: spec.Title, Tasks: tasks}, nil
}

// blockers returns what blocks task: its blocked-by links, then the issues its
// body names that are not already linked. A named issue's state comes from
// the spec's own tasks when it is one of them, and from gh otherwise.
func (g GitHub) blockers(ctx context.Context, task issue, tasks map[int]issue) ([]Blocker, error) {
	var blockers []Blocker
	linked := map[int]bool{}
	for _, node := range task.BlockedBy.Nodes {
		linked[node.Number] = true
		blockers = append(blockers, Blocker{Number: node.Number, Open: isOpen(node.State)})
	}
	for _, n := range blockersIn(task.Body) {
		if linked[n] {
			continue
		}
		named, ok := tasks[n]
		if !ok {
			var err error
			named, err = g.view(ctx, n, "number,state")
			if err != nil {
				return nil, fmt.Errorf("read blocker of #%d: %w", task.Number, err)
			}
		}
		blockers = append(blockers, Blocker{Number: n, Open: isOpen(named.State)})
	}
	return blockers, nil
}

// view reads issue number's fields through gh, telling an issue gh has no
// record of apart from gh not being usable at all.
func (g GitHub) view(ctx context.Context, number int, fields string) (issue, error) {
	var stdout, stderr bytes.Buffer
	if err := g.gh.Run(ctx, "gh", []string{"issue", "view", strconv.Itoa(number), "--json", fields}, nil, &stdout, &stderr); err != nil {
		if strings.Contains(stderr.String(), notFoundMark) {
			return issue{}, &NotFoundError{Number: number}
		}
		return issue{}, &UnavailableError{Tool: "gh", Detail: stderr.String(), Err: err}
	}
	var got issue
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		return issue{}, fmt.Errorf("read gh's answer for #%d: %w", number, err)
	}
	return got, nil
}

// linked returns every issue linked to issue number over the named connection.
// When gh's view holds only the first page of them, it reads them all again
// page by page, so a cut-off list is never taken for the whole one.
func (g GitHub) linked(ctx context.Context, number int, name string, viewed connection) ([]issueNode, error) {
	if len(viewed.Nodes) >= viewed.TotalCount {
		return viewed.Nodes, nil
	}
	var nodes []issueNode
	cursor := ""
	for {
		page, err := g.linkedPage(ctx, number, name, cursor)
		if err != nil {
			return nil, err
		}
		got, ok := page.Data.Repository.Issue[name]
		if !ok {
			return nil, fmt.Errorf("read %s of #%d: gh's answer holds none", name, number)
		}
		nodes = append(nodes, got.Nodes...)
		if !got.PageInfo.HasNextPage {
			return nodes, nil
		}
		cursor = got.PageInfo.EndCursor
	}
}

// linkedPage reads the page of issue number's linked issues over the named
// connection that follows cursor, or the first page when cursor is empty.
func (g GitHub) linkedPage(ctx context.Context, number int, name, cursor string) (linkedPage, error) {
	args := []string{"api", "graphql",
		"-F", "owner={owner}", "-F", "repo={repo}", "-F", "number=" + strconv.Itoa(number),
		"-f", "query=" + fmt.Sprintf(linkedQuery, name)}
	if cursor != "" {
		args = append(args, "-f", "endCursor="+cursor)
	}
	var stdout, stderr bytes.Buffer
	if err := g.gh.Run(ctx, "gh", args, nil, &stdout, &stderr); err != nil {
		return linkedPage{}, &UnavailableError{Tool: "gh", Detail: stderr.String(), Err: err}
	}
	var page linkedPage
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		return linkedPage{}, fmt.Errorf("read gh's answer for the %s of #%d: %w", name, number, err)
	}
	return page, nil
}

// labelled reports whether the issue carries the label called name.
func (i issue) labelled(name string) bool {
	return slices.Contains(i.Labels, label{Name: name})
}

// audience is who the issue is meant for. A nested spec is never a task for
// either, and an issue labelled for both is kept for the human.
func (i issue) audience() Audience {
	switch {
	case i.labelled(specLabel):
		return Nobody
	case i.labelled(humanLabel):
		return Human
	case i.labelled(agentLabel):
		return Agent
	}
	return Nobody
}

// isOpen reports whether a GitHub issue state is open.
func isOpen(state string) bool {
	return strings.EqualFold(state, "open")
}

// listedOrder orders children with those listed first, in listed order, then
// the rest as they came. A listed number that is not a child is dropped.
func listedOrder(listed, children []int) []int {
	ordered := make([]int, 0, len(children))
	for _, n := range listed {
		if slices.Contains(children, n) {
			ordered = append(ordered, n)
		}
	}
	for _, n := range children {
		if !slices.Contains(ordered, n) {
			ordered = append(ordered, n)
		}
	}
	return ordered
}
