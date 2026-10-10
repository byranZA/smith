package bootstrap

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestScriptPartsJoinInListedOrder proves the shipped script is its parts
// joined in the listed order rather than the directory's, a blank line apart.
func TestScriptPartsJoinInListedOrder(t *testing.T) {
	parts := fstest.MapFS{
		"script/a.sh": {Data: []byte("main \"$@\"\n")},
		"script/b.sh": {Data: []byte("#!/usr/bin/env bash\n")},
		"script/c.sh": {Data: []byte("f() { :; }\n")},
	}

	got := mustJoinScriptParts(parts, []string{"b.sh", "c.sh", "a.sh"})

	want := "#!/usr/bin/env bash\n\nf() { :; }\n\nmain \"$@\"\n"
	if got != want {
		t.Errorf("joined script = %q, want %q", got, want)
	}
}

// TestScriptPartsMismatchPanics proves package init panics, naming the part,
// when the ordered list and the part files on disk disagree in either
// direction, so a part can be neither dropped nor shipped unlisted.
func TestScriptPartsMismatchPanics(t *testing.T) {
	parts := fstest.MapFS{
		"script/core.sh": {Data: []byte("#!/usr/bin/env bash\n")},
		"script/main.sh": {Data: []byte("main \"$@\"\n")},
	}
	tests := []struct {
		name  string
		order []string
		want  string
	}{
		{"part left out of the list", []string{"core.sh"}, "main.sh"},
		{"listed part missing from disk", []string{"core.sh", "probe.sh", "main.sh"}, "probe.sh"},
		{"part listed twice", []string{"core.sh", "main.sh", "core.sh"}, "core.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("mustJoinScriptParts did not panic")
				}
				if msg, _ := r.(string); !strings.Contains(msg, tt.want) {
					t.Errorf("panic = %v, want it to name %q", r, tt.want)
				}
			}()
			mustJoinScriptParts(parts, tt.order)
		})
	}
}

// TestScriptIsFramedByTheShebangAndTheMainCall proves the shipped script is
// assembled from the embedded parts in order, opening with the shebang and
// ending with the call that runs main.
func TestScriptIsFramedByTheShebangAndTheMainCall(t *testing.T) {
	if !strings.HasPrefix(Script, "#!/usr/bin/env bash\n") {
		t.Errorf("Script does not open with the shebang: %q", Script[:min(len(Script), 40)])
	}
	if !strings.HasSuffix(Script, "\nmain \"$@\"\n") {
		t.Errorf("Script does not end by running main: %q", Script[max(0, len(Script)-40):])
	}
}
