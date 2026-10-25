package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestHelpExitCodesExplainsEveryCode(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"help", "exit-codes"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errBuf.String())
	}
	for _, code := range []string{"0", "1", "2", "3", "4", "5", "6", "127"} {
		explained := regexp.MustCompile(`(?m)^\s*` + code + `\s+\S`)
		if !explained.MatchString(out.String()) {
			t.Errorf("smith help exit-codes = %q, want a line explaining exit code %s", out.String(), code)
		}
	}
}

func TestHelpExitCodesExplainsInterruptionsOfAnyVerb(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"help", "exit-codes"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errBuf.String())
	}
	for code, signal := range map[string]string{"129": "SIGHUP", "130": "SIGINT", "143": "SIGTERM"} {
		explained := regexp.MustCompile(`(?m)^\s*` + code + `\s+.*` + signal)
		if !explained.MatchString(out.String()) {
			t.Errorf("smith help exit-codes = %q, want a line explaining exit code %s as %s", out.String(), code, signal)
		}
	}
	if !strings.Contains(out.String(), "any verb") {
		t.Errorf("smith help exit-codes = %q, want it to say an interruption can end any verb", out.String())
	}
}

func TestExitCodesIsAHelpTopic(t *testing.T) {
	topic, _, err := newRootCmd().Find([]string{"exit-codes"})
	if err != nil {
		t.Fatalf("Find(exit-codes) err = %v", err)
	}
	if !topic.IsAdditionalHelpTopicCommand() {
		t.Errorf("exit-codes is not an additional help topic, want a help-only command with no Run")
	}
}

// exampleBox is the box name every relaying verb's Example uses for its with-box form.
const exampleBox = "devbox"

// leaves returns every runnable, visible leaf command under cmd, keyed by its full command path.
func leaves(cmd *cobra.Command) map[string]*cobra.Command {
	found := map[string]*cobra.Command{}
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
			continue
		}
		if child.HasSubCommands() {
			for path, leaf := range leaves(child) {
				found[path] = leaf
			}
			continue
		}
		if child.Runnable() {
			found[child.CommandPath()] = child
		}
	}
	return found
}

func TestEveryVerbHasLongHelp(t *testing.T) {
	for path, verb := range leaves(newRootCmd()) {
		if strings.TrimSpace(verb.Long) == "" {
			t.Errorf("%s has no Long, want help saying what the verb does", path)
		}
	}
}

func TestEveryVerbHasAnExample(t *testing.T) {
	for path, verb := range leaves(newRootCmd()) {
		if strings.TrimSpace(verb.Example) == "" {
			t.Errorf("%s has no Example, want at least one realistic invocation", path)
		}
	}
}

func TestEveryVerbPointsToTheExitCodesTopic(t *testing.T) {
	for path, verb := range leaves(newRootCmd()) {
		if !strings.Contains(verb.Long, `see "smith help exit-codes"`) {
			t.Errorf("%s Long = %q, want it to name its exit codes and point to smith help exit-codes", path, verb.Long)
		}
	}
}

func TestEveryRelayingVerbExamplesBothForms(t *testing.T) {
	relaying := 0
	for path, verb := range leaves(newRootCmd()) {
		if !strings.Contains(verb.Use, "[<box>]") {
			continue
		}
		relaying++
		var onBox, relayed bool
		for _, line := range strings.Split(verb.Example, "\n") {
			args, ok := strings.CutPrefix(strings.TrimSpace(line), path)
			if !ok {
				continue
			}
			fields := strings.Fields(args)
			if len(fields) > 0 && fields[0] == exampleBox {
				relayed = true
				continue
			}
			onBox = true
		}
		if !onBox || !relayed {
			t.Errorf("%s Example = %q, want an on-box form and a form relayed to box %s", path, verb.Example, exampleBox)
		}
	}
	if relaying == 0 {
		t.Error("found no verb whose Use takes [<box>], want the relaying verbs walked")
	}
}
