package workbench

import (
	"strconv"
	"strings"
)

// slugMax bounds how many characters of the issue's title a work branch's
// name carries.
const slugMax = 40

// specPrefix is the label a spec's title leads with, which says nothing a
// work branch's name needs.
const specPrefix = "spec:"

// Branch names the work branch of issue number titled title:
// smith/<number>-<slug>, the slug being the title lower-cased and hyphenated,
// without a leading "Spec:", and cut at a word to at most 40 characters.
func Branch(number int, title string) string {
	name := "smith/" + strconv.Itoa(number)
	if slug := slugOf(title); slug != "" {
		name += "-" + slug
	}
	return name
}

// slugOf returns title lower-cased, with a leading "spec:" dropped, each run
// of characters other than ASCII letters and digits made one hyphen, and cut
// to slugMax at the last hyphen when that would split a word.
func slugOf(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	lower = strings.TrimPrefix(lower, specPrefix)
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if len(slug) <= slugMax {
		return slug
	}
	cut := slug[:slugMax]
	if slug[slugMax] != '-' {
		if i := strings.LastIndexByte(cut, '-'); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimSuffix(cut, "-")
}
