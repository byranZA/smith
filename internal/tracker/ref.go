package tracker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// refForm matches the ways an operator names an issue: 42, #42, or the
// issue's URL.
var refForm = regexp.MustCompile(`^(?:#|https?://[^/]+/[^/]+/[^/]+/issues/)?(\d+)/?$`)

// ParseRef returns the issue number ref names, written as 42, #42 or the
// issue's URL.
func ParseRef(ref string) (int, error) {
	m := refForm.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return 0, fmt.Errorf("%q is not an issue: name it as 42, #42 or the issue URL", ref)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%q is not an issue number", ref)
	}
	return n, nil
}
