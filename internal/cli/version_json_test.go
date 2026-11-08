package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runVersion(t *testing.T, version string, stamp buildStamp, args ...string) (string, string) {
	t.Helper()
	cmd := newVersionCmd(version, stamp)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version %v err = %v", args, err)
	}
	return out.String(), errBuf.String()
}

func TestVersionJSONDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		stamp   buildStamp
		want    string
	}{
		{"release build reports version and provenance", "1.2.3", buildStamp{commit: "0123456789abcdef", date: "2026-11-01T10:00:00Z"},
			`{"schema_version":1,"version":"1.2.3","commit":"0123456789abcdef","built_at":"2026-11-01T10:00:00Z"}` + "\n"},
		{"build with no VCS stamp omits provenance", "1.2.3", buildStamp{},
			`{"schema_version":1,"version":"1.2.3"}` + "\n"},
		{"development build says dev", "dev", buildStamp{},
			`{"schema_version":1,"version":"dev"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, errOut := runVersion(t, tt.version, tt.stamp, "--json")
			if out != tt.want {
				t.Errorf("stdout = %q, want %q", out, tt.want)
			}
			if errOut != "" {
				t.Errorf("stderr = %q, want empty", errOut)
			}
		})
	}
}

func TestVersionWithoutJSONStaysProse(t *testing.T) {
	t.Parallel()
	out, _ := runVersion(t, "1.2.3", buildStamp{commit: "0123456789abcdef"})
	if want := "smith 1.2.3\ncommit: 0123456\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func jsonVerbs(root *cobra.Command) []*cobra.Command {
	var verbs []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("json") != nil {
			verbs = append(verbs, c)
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	return verbs
}

func TestEveryJSONVerbIsDocumentedInHelpJSON(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	verbs := jsonVerbs(root)
	if len(verbs) == 0 {
		t.Fatal("no verb accepts --json")
	}
	root.SetArgs([]string{"help", "json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("help json: %v", err)
	}
	for _, v := range verbs {
		if !strings.Contains(out.String(), "`"+v.CommandPath()+"`") {
			t.Errorf("smith help json does not document %q", v.CommandPath())
		}
		if !strings.Contains(v.Flags().Lookup("json").Usage, "smith help json") {
			t.Errorf("%s --json help does not point to smith help json", v.CommandPath())
		}
	}
}

func TestHelpJSONStatesTheChangePolicy(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	root.SetArgs([]string{"help", "json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("help json: %v", err)
	}
	for _, want := range []string{"schema_version", "may gain fields", "breaking"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("smith help json lacks %q", want)
		}
	}
}

func TestAgentGuideNamesJSONAndItsTopic(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	root.SetArgs([]string{"help", "agents"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("help agents: %v", err)
	}
	for _, want := range []string{"--json", "smith help json"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("smith help agents lacks %q", want)
		}
	}
}
