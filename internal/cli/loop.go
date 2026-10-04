package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// loopWiring is what the loop verbs reach the outside world through: gh for
// the tracker, the launcher that starts an agent, and the PATH search that
// finds one.
type loopWiring struct {
	gh       tracker.Runner
	launcher loop.Launcher
	lookPath func(name string) (string, error)
}

// systemLoop is the loop wiring of the operator's own machine: the real gh,
// agents started as processes streaming to smith's own output, and the real
// PATH.
func systemLoop() loopWiring {
	return loopWiring{
		gh:       connection.System(),
		launcher: loop.Process{Stdout: os.Stdout, Stderr: os.Stderr},
		lookPath: exec.LookPath,
	}
}

// newLoopCmd builds `smith loop`, the verbs that work a spec's tasks in the
// repo smith is run from, reading the tracker through gh. None is relayed to a
// box: each runs where smith is invoked.
func newLoopCmd(w loopWiring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop",
		Short: "Work a spec's tasks with a coding agent, one task at a time",
	}
	cmd.AddCommand(newLoopListCmd(tracker.NewGitHub(w.gh)), newLoopRunCmd(w))
	return cmd
}

// newLoopRunCmd builds `smith loop run <spec>`, which works the spec's tasks
// unattended, one agent run per task, until none is available for an agent.
// An agent missing from the PATH is refused before the tracker is read. Exit 0
// only when the spec is complete; otherwise the report names what was left.
func newLoopRunCmd(w loopWiring) *cobra.Command {
	return &cobra.Command{
		Use:   "run <spec>",
		Short: "Work a spec's tasks to completion with a coding agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			adapter, err := agent.Lookup(agent.Default)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := w.lookPath(agent.Default); err != nil {
				return reportInvalid(cmd, fmt.Errorf("agent %s is not installed: %w", agent.Default, err))
			}
			l := loop.Loop{
				Tracker:  tracker.NewGitHub(w.gh),
				Launcher: w.launcher,
				Agent:    adapter,
				Prompt:   loop.BuiltinPrompt(),
				Progress: cmd.ErrOrStderr(),
			}
			outcome, err := l.Run(cmd.Context(), number)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), runReport(number, outcome)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if !outcome.Complete() {
				return &exitError{code: 1}
			}
			return nil
		},
	}
}

// runReport renders how a run ended: the spec complete, or why it stopped and
// what remains open.
func runReport(spec int, outcome loop.Outcome) string {
	if outcome.Complete() {
		return fmt.Sprintf("spec #%d complete\n", spec)
	}
	var b strings.Builder
	if len(outcome.LeftOpen) > 0 {
		fmt.Fprintf(&b, "spec #%d stopped: the agent left %s open\n", spec, taskList(outcome.LeftOpen))
	} else {
		fmt.Fprintf(&b, "spec #%d stopped: no task is available for an agent\n", spec)
	}
	writeKinds(&b, outcome.Remaining, false)
	return b.String()
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
	writeKinds(&b, remaining, true)
	return b.String()
}

// writeKinds writes one line for each kind of task that remains, leaving out
// the kinds it has none of, and the closed tasks unless withClosed is set.
func writeKinds(b *strings.Builder, remaining loop.Remaining, withClosed bool) {
	var closed []tracker.Task
	if withClosed {
		closed = remaining.Closed
	}
	for _, kind := range []struct {
		name  string
		tasks []tracker.Task
	}{
		{"available", remaining.Available},
		{"blocked", remaining.Blocked},
		{"waiting on a human", remaining.Human},
		{"not ready for an agent or a human", remaining.Unready},
		{"closed", closed},
	} {
		if len(kind.tasks) > 0 {
			fmt.Fprintf(b, "  %s: %s\n", kind.name, taskList(kind.tasks))
		}
	}
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
