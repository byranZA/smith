package tracker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Ref names an issue: its number, and the repo it is in when the reference
// says, as owner/name. An empty Repo is the repo smith is run in.
type Ref struct {
	Repo   string
	Number int
}

// String writes ref as GitHub does: #42 here, owner/name#42 in a named repo.
func (r Ref) String() string {
	return fmt.Sprintf("%s#%d", r.Repo, r.Number)
}

// refForm matches the ways an operator names an issue: 42, #42,
// owner/name#42, or the issue's URL, capturing the owner/name it gives.
var refForm = regexp.MustCompile(`^(?:#|([^/#\s]+/[^/#\s]+)#|https?://[^/]+/([^/]+/[^/]+)/issues/)?(\d+)/?$`)

// ParseRef returns the issue ref names, written as 42, #42, owner/name#42 or
// the issue's URL. A qualified ref or URL keeps the repo it names, so the
// tracker can tell it from the repo smith is run in.
func ParseRef(ref string) (Ref, error) {
	m := refForm.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return Ref{}, fmt.Errorf("%q is not an issue: name it as 42, #42, owner/name#42 or the issue URL", ref)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return Ref{}, fmt.Errorf("%q is not an issue number: %w", ref, err)
	}
	if n == 0 {
		return Ref{}, fmt.Errorf("%q is not an issue number", ref)
	}
	return Ref{Repo: m[1] + m[2], Number: n}, nil
}

// in returns ref as seen from repo: with no Repo when it is in repo, so the
// same issue named two ways is one Ref.
func (r Ref) in(repo string) Ref {
	if strings.EqualFold(r.Repo, repo) {
		r.Repo = ""
	}
	return r
}
