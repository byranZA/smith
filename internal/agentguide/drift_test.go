package agentguide_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agentguide"
)

// glossaryTerms returns the terms the guide's "## Glossary" section defines, each written as a "- **term**:" list item.
func glossaryTerms(t *testing.T, prose string) []string {
	t.Helper()
	_, after, ok := strings.Cut(prose, "\n## Glossary\n")
	if !ok {
		t.Fatal("the guide has no \"## Glossary\" section")
	}
	section, _, _ := strings.Cut(after, "\n## ")
	var terms []string
	for _, m := range regexp.MustCompile(`(?m)^- \*\*([^*]+)\*\*:`).FindAllStringSubmatch(section, -1) {
		terms = append(terms, m[1])
	}
	return terms
}

// domainTerms returns every term CONTEXT.md defines, lower-cased, splitting a "Outcome / stop report" entry into its names.
func domainTerms(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../CONTEXT.md")
	if err != nil {
		t.Fatalf("read CONTEXT.md: %v", err)
	}
	terms := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\*\*(.+)\*\*:$`).FindAllStringSubmatch(string(raw), -1) {
		for _, name := range strings.Split(m[1], " / ") {
			terms[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	return terms
}

func TestGlossaryTermsAreDefinedInContext(t *testing.T) {
	t.Parallel()
	known := domainTerms(t)
	terms := glossaryTerms(t, agentguide.Prose())
	if len(terms) < 12 {
		t.Errorf("glossary has %d terms, want at least 12: %v", len(terms), terms)
	}
	for _, term := range terms {
		if !known[strings.ToLower(term)] {
			t.Errorf("glossary term %q is not a term CONTEXT.md defines", term)
		}
	}
}

func TestGlossaryCheckCatchesAnUnknownTerm(t *testing.T) {
	t.Parallel()
	got := glossaryTerms(t, "\n## Glossary\n\n- **Zorp**: nothing.\n- **Box**: a box.\n\n## Next\n")
	known := domainTerms(t)
	if len(got) != 2 || known[strings.ToLower(got[0])] || !known[strings.ToLower(got[1])] {
		t.Errorf("glossaryTerms/domainTerms = %v; want Zorp unknown and Box known", got)
	}
}
