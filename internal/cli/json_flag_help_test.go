package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestJSONFlagHelpLineHasNoValueName(t *testing.T) {
	t.Parallel()
	cmd := newVersionCmd("1.2.3", buildStamp{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --help err = %v", err)
	}
	want := `--json   print the answer as one JSON document on stdout; see "smith help json"`
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--json") {
			if !strings.Contains(line, want) {
				t.Errorf("--json help line = %q, want it to contain %q", strings.TrimSpace(line), want)
			}
			return
		}
	}
	t.Fatalf("no --json line in help:\n%s", out.String())
}
