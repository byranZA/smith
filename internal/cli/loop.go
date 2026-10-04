package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

// loopWiring is what the loop verbs reach the outside world through: gh for
// the tracker, the launcher that starts an agent, the PATH search that finds
// one, and git, the config home and the working directory that locate the
// repo file.
type loopWiring struct {
	gh       tracker.Runner
	launcher loop.Launcher
	lookPath func(name string) (string, error)
	git      repofile.Runner
	home     homeResolver
	workdir  workdirResolver
}

// systemLoop is the loop wiring of the operator's own machine: the real gh,
// agents started as processes on smith's own terminal, and the real PATH.
func systemLoop() loopWiring {
	return loopWiring{
		gh:       connection.System(),
		launcher: loop.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
		lookPath: exec.LookPath,
		git:      connection.System(),
		home:     userConfigHome,
		workdir:  currentDir,
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
	cmd.AddCommand(newLoopListCmd(w), newLoopRunCmd(w), newLoopPromptCmd())
	return cmd
}

// repo locates the git repo smith is run from, the one whose repo file and
// ejected loop prompt the loop verbs read.
func (w loopWiring) repo(ctx context.Context) (repofile.Repo, error) {
	home, err := w.home()
	if err != nil {
		return repofile.Repo{}, err
	}
	dir, err := w.workdir()
	if err != nil {
		return repofile.Repo{}, err
	}
	repo, err := repofile.Locate(ctx, w.git, dir, home)
	if err != nil {
		return repofile.Repo{}, fmt.Errorf("locate repo: %w", err)
	}
	return repo, nil
}

// settings resolves the loop settings for the repo smith is run from, flags
// over its repo file over the built-in defaults. A repo with no repo file
// resolves to the defaults.
func (w loopWiring) settings(ctx context.Context, flags repofile.File) (loop.Settings, error) {
	repo, err := w.repo(ctx)
	if err != nil {
		return loop.Settings{}, fmt.Errorf("resolve loop settings: %w", err)
	}
	file, err := repofile.Load(repo)
	if err != nil {
		return loop.Settings{}, fmt.Errorf("resolve loop settings: %w", err)
	}
	settings, err := loop.ResolveSettings(flags, file)
	if err != nil {
		return loop.Settings{}, fmt.Errorf("resolve loop settings: %w", err)
	}
	return settings, nil
}

// prompt loads the loop prompt for the repo smith is run from: its ejected
// copy when it has one, otherwise the built-in.
func (w loopWiring) prompt(ctx context.Context) (loop.Prompt, error) {
	repo, err := w.repo(ctx)
	if err != nil {
		return "", fmt.Errorf("load loop prompt: %w", err)
	}
	prompt, err := loop.LoadPrompt(repo.PromptPath())
	if err != nil {
		return "", fmt.Errorf("load loop prompt: %w", err)
	}
	return prompt, nil
}

// settingsReport renders each resolved setting with where it came from,
// naming an unset model or effort as the agent's own default.
func settingsReport(s loop.Settings) string {
	var b strings.Builder
	for _, setting := range []struct {
		name  string
		value config.Value
	}{
		{"agent", s.Agent},
		{"model", s.Model},
		{"effort", s.Effort},
	} {
		if setting.value.Value == "" {
			setting.value.Value = "the agent's own default"
		}
		fmt.Fprintf(&b, "%-7s %s\n", setting.name+":", setting.value)
	}
	return b.String()
}

// newLoopPromptCmd builds `smith loop prompt`, which prints the built-in loop
// prompt with its placeholders unfilled, whether or not the repo has ejected
// its own. Ejecting is saving this output as .smith/prompt.md, which smith
// itself never writes.
func newLoopPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Print the built-in loop prompt, to eject or diff against",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprint(cmd.OutOrStdout(), loop.BuiltinPrompt()); err != nil {
				return fmt.Errorf("write loop prompt: %w", err)
			}
			return nil
		},
	}
}

// newLoopRunCmd builds `smith loop run <spec>`, which works the spec's tasks
// unattended, one agent run per task, until none is available for an agent.
// --agent, --model and --effort override the repo file for this run only, and
// the run first reports each setting with where it came from. A task the agent
// leaves open is retried up to --max-attempts, then skipped, and
// --max-iterations caps the agent runs. --interactive instead hands the next
// task alone to the agent attached to the terminal. Limits below one, invalid
// settings and an agent missing from the PATH are refused before the tracker
// is read. Exit 0 only when the spec is complete, or when an interactive run's
// task was closed; otherwise the report names what was left and why. The
// repo's ejected .smith/prompt.md, when it has one, replaces the built-in loop
// prompt.
func newLoopRunCmd(w loopWiring) *cobra.Command {
	limits := loop.DefaultLimits()
	var flags repofile.File
	var interactive bool
	cmd := &cobra.Command{
		Use:   "run <spec>",
		Short: "Work a spec's tasks to completion with a coding agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if err := checkLimits(limits); err != nil {
				return reportInvalid(cmd, err)
			}
			settings, err := w.settings(cmd.Context(), flags)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			prompt, err := w.prompt(cmd.Context())
			if err != nil {
				return reportInvalid(cmd, err)
			}
			adapter, err := agent.Lookup(settings.Agent.Value)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := w.lookPath(settings.Agent.Value); err != nil {
				return reportInvalid(cmd, fmt.Errorf("agent %s is not installed: %w", settings.Agent.Value, err))
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), settingsReport(settings)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			l := loop.Loop{
				Tracker:  tracker.NewGitHub(w.gh),
				Launcher: w.launcher,
				Agent:    adapter,
				Options:  settings.Options(),
				Prompt:   prompt,
				Limits:   limits,
				Progress: cmd.ErrOrStderr(),
			}
			report, succeeded, err := runLoop(cmd.Context(), l, number, interactive)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), report); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if !succeeded {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.Agent, "agent", "", "the agent to hand each task to, over the repo file")
	cmd.Flags().StringVar(&flags.Model, "model", "", "the model the agent runs, over the repo file")
	cmd.Flags().StringVar(&flags.Effort, "effort", "", "how hard the agent thinks (low, medium, high), over the repo file")
	cmd.Flags().IntVar(&limits.MaxIterations, "max-iterations", limits.MaxIterations, "most agent runs the loop makes in all")
	cmd.Flags().IntVar(&limits.MaxAttempts, "max-attempts", limits.MaxAttempts, "most agent runs one task gets before it is skipped")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "run the next task alone, with the agent attached to this terminal")
	return cmd
}

// runLoop works spec with l, unattended to completion or, when interactive,
// one task attached to the terminal, and returns the report of how it ended
// and whether it succeeded: the spec complete, or the interactive task closed.
func runLoop(ctx context.Context, l loop.Loop, spec int, interactive bool) (string, bool, error) {
	if !interactive {
		outcome, err := l.Run(ctx, spec)
		if err != nil {
			return "", false, fmt.Errorf("work spec #%d: %w", spec, err)
		}
		return runReport(spec, l.Limits, outcome), outcome.Complete(), nil
	}
	outcome, err := l.RunInteractive(ctx, spec)
	if err != nil {
		return "", false, fmt.Errorf("work spec #%d interactively: %w", spec, err)
	}
	return interactiveReport(spec, outcome), outcome.Succeeded(), nil
}

// interactiveReport renders how an interactive run ended: whether the task it
// handed was closed or, with no task to hand, why not.
func interactiveReport(spec int, outcome loop.InteractiveOutcome) string {
	switch {
	case !outcome.Handed:
		return runReport(spec, loop.Limits{}, loop.Outcome{Remaining: outcome.Remaining})
	case outcome.Closed():
		return fmt.Sprintf("task #%d closed\n", outcome.Task.Number)
	default:
		return fmt.Sprintf("task #%d still open\n", outcome.Task.Number)
	}
}

// checkLimits refuses a limit below one, naming its flag.
func checkLimits(limits loop.Limits) error {
	if limits.MaxIterations < 1 {
		return fmt.Errorf("--max-iterations must be at least 1, got %d", limits.MaxIterations)
	}
	if limits.MaxAttempts < 1 {
		return fmt.Errorf("--max-attempts must be at least 1, got %d", limits.MaxAttempts)
	}
	return nil
}

// runReport renders how a run ended: the spec complete, or why it stopped and
// what remains open, naming the tasks it skipped and the attempts each had.
func runReport(spec int, limits loop.Limits, outcome loop.Outcome) string {
	if outcome.Complete() {
		return fmt.Sprintf("spec #%d complete\n", spec)
	}
	var b strings.Builder
	switch {
	case outcome.Capped:
		fmt.Fprintf(&b, "spec #%d stopped: reached the cap of %d agent runs\n", spec, limits.MaxIterations)
	case len(outcome.Remaining.Skipped) > 0:
		fmt.Fprintf(&b, "spec #%d stopped: the agent left a task open on every attempt\n", spec)
	default:
		fmt.Fprintf(&b, "spec #%d stopped: no task is available for an agent\n", spec)
	}
	writeKinds(&b, outcome.Remaining, limits, false)
	return b.String()
}

// newLoopListCmd builds `smith loop list <spec>`, which names the spec's next
// available task and sorts what remains, after the loop settings the repo
// file resolves to, running no agent and changing nothing in the tracker. Exit 0 once the spec is read, whatever it holds; a
// spec that cannot be read is reported naming why.
func newLoopListCmd(w loopWiring) *cobra.Command {
	return &cobra.Command{
		Use:   "list <spec>",
		Short: "Show the next task the loop would run, and what remains",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			settings, err := w.settings(cmd.Context(), repofile.File{})
			if err != nil {
				return reportInvalid(cmd, err)
			}
			spec, err := tracker.NewGitHub(w.gh).Spec(cmd.Context(), number)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), settingsReport(settings)+listReport(spec, loop.Survey(spec))); err != nil {
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
	writeKinds(&b, remaining, loop.Limits{}, true)
	return b.String()
}

// writeKinds writes one line for each kind of task that remains, leaving out
// the kinds it has none of, and the closed tasks unless withClosed is set.
// Skipped tasks are named with the attempts limits allowed them.
func writeKinds(b *strings.Builder, remaining loop.Remaining, limits loop.Limits, withClosed bool) {
	var closed []tracker.Task
	if withClosed {
		closed = remaining.Closed
	}
	for _, kind := range []struct {
		name  string
		tasks []tracker.Task
	}{
		{"available", remaining.Available},
		{fmt.Sprintf("skipped after %d attempts", limits.MaxAttempts), remaining.Skipped},
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
