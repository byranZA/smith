package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// newLoopCmd builds `smith loop`, the verbs that work a spec's tasks in the
// repo smith is run from, reading the tracker through gh. None is relayed to a
// box: each runs where smith is invoked.
func newLoopCmd(gh tracker.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop",
		Short: "Work a spec's tasks with a coding agent, one task at a time",
	}
	cmd.AddCommand(newLoopListCmd(tracker.NewGitHub(gh)))
	return cmd
}

// newLoopListCmd builds `smith loop list <spec>`, which names the spec's next
// available task and sorts what remains, running no agent and changing
// nothing in the tracker. Exit 0 once the spec is read, whatever it holds; a
// spec that cannot be read is reported naming why.
func newLoopListCmd(tr tracker.GitHub) *cobra.Command {
	return &cobra.Command{
		Use:   "list <spec>",
		Short: "Show the next task the loop would run, and what remains",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			spec, err := tr.Spec(cmd.Context(), number)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), listReport(spec, loop.Survey(spec))); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
}

// listReport renders the spec, its next task, and one line for each kind of
// task that remains, leaving out the kinds it has none of.
func listReport(spec tracker.Spec, remaining loop.Remaining) string {
	var b strings.Builder
	fmt.Fprintf(&b, "spec #%d %s\n", spec.Number, spec.Title)
	switch next, ok := remaining.Next(); {
	case ok:
		fmt.Fprintf(&b, "next: #%d %s\n", next.Number, next.Title)
	case remaining.Complete():
		b.WriteString("next: none, spec complete\n")
	default:
		b.WriteString("next: none\n")
	}
	for _, kind := range []struct {
		name  string
		tasks []tracker.Task
	}{
		{"available", remaining.Available},
		{"blocked", remaining.Blocked},
		{"waiting on a human", remaining.Human},
		{"not ready for an agent or a human", remaining.Unready},
		{"closed", remaining.Closed},
	} {
		if len(kind.tasks) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", kind.name, taskList(kind.tasks))
		}
	}
	return b.String()
}

// taskList renders tasks as "#43, #44", naming each blocked task's open
// blockers after it.
func taskList(tasks []tracker.Task) string {
	refs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ref := fmt.Sprintf("#%d", task.Number)
		var open []string
		for _, b := range task.Blockers {
			if b.Open {
				open = append(open, fmt.Sprintf("#%d", b.Number))
			}
		}
		if len(open) > 0 {
			ref += " (by " + strings.Join(open, ", ") + ")"
		}
		refs = append(refs, ref)
	}
	return strings.Join(refs, ", ")
}
