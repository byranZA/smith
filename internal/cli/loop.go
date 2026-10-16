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
	"github.com/byranZA/smith/internal/workbench"
)

// loopWiring is what the loop verbs reach the outside world through: gh for
// the tracker, the launcher that starts an agent, the bench that sets up an
// execution's work branch and task worktrees in the repo a directory is in
// and finds how far an adopted one is behind its base, the pusher that pushes
// the work branch from a directory, the PATH search that finds an agent, the
// hostname that names this machine on a skipped task, and git, the config home
// and the working directory that locate the repo file.
type loopWiring struct {
	gh       tracker.Runner
	launcher loop.Launcher
	bench    func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error)
	pusher   func(dir string) loop.Pusher
	lookPath func(name string) (string, error)
	hostname func() (string, error)
	git      repofile.Runner
	home     homeResolver
	workdir  workdirResolver
}

// systemLoop is the loop wiring of the operator's own machine: the real gh,
// agents started as processes on smith's own terminal, the real git keeping
// the workbench and pushing to origin, the real PATH and the real hostname.
func systemLoop() loopWiring {
	return loopWiring{
		gh:       connection.System(),
		launcher: loop.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
		bench:    systemBench,
		pusher:   func(dir string) loop.Pusher { return loop.Origin{Git: connection.System(), Dir: dir} },
		lookPath: exec.LookPath,
		hostname: os.Hostname,
		git:      connection.System(),
		home:     userConfigHome,
		workdir:  currentDir,
	}
}

// systemBench sets up the workbench for req in the repo dir is in, with the
// real git, and returns how far an adopted work branch is behind its base.
func systemBench(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
	bench, err := workbench.Setup(ctx, connection.System(), dir, req)
	if err != nil {
		return nil, workbench.Lag{}, fmt.Errorf("set up the work branch for #%d: %w", req.Number, err)
	}
	return bench, bench.Lag(), nil
}

// newLoopCmd builds `smith loop`, the verbs that work a spec's tasks, or a
// single task, in the
// repo smith is run from, reading the tracker through gh. None is relayed to a
// box: each runs where smith is invoked.
func newLoopCmd(w loopWiring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop",
		Short: "Work a spec's tasks, or a single task, with a coding agent, one task at a time",
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

// origin returns the pusher that pushes the work branch from the repo smith
// is run in, or nil when settings turn pushing off.
func (w loopWiring) origin(settings loop.Settings) (loop.Pusher, error) {
	if !settings.Pushes() {
		return nil, nil
	}
	dir, err := w.workdir()
	if err != nil {
		return nil, fmt.Errorf("find the repo to push from: %w", err)
	}
	return w.pusher(dir), nil
}

// workbench sets up the work branch for execution in the repo smith is run
// in, the one branch names or, when it is empty, the execution's own, cutting a
// missing one from base, or from origin's default branch when base is empty,
// before any agent runs, and returns how far an adopted one is behind its base.
func (w loopWiring) workbench(ctx context.Context, execution tracker.Execution, branch, base string) (loop.Bench, workbench.Lag, error) {
	dir, err := w.workdir()
	if err != nil {
		return nil, workbench.Lag{}, fmt.Errorf("find the repo to work in: %w", err)
	}
	return w.bench(ctx, dir, workbench.Request{Number: execution.Number, Title: execution.Title, Branch: branch, Base: base})
}

// warnOfLabels writes a warning for each triage label the repo file sets that
// the repo lacks, or for a label check that could not run, and goes on.
func warnOfLabels(cmd *cobra.Command, repo loop.LabelLister, s loop.Settings) error {
	for _, warning := range loop.LabelWarnings(cmd.Context(), repo, s) {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "smith: warning: %s\n", warning); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return nil
}

// lagWarning renders the warning line for a work branch with lag behind its
// base, or that its lag could not be found, and is empty when neither holds.
func lagWarning(branch string, lag workbench.Lag) string {
	if lag.Err != nil {
		return fmt.Sprintf("smith: warning: could not check whether work branch %s is behind origin/%s: %v\n", branch, lag.Base, lag.Err)
	}
	if lag.Commits == 0 {
		return ""
	}
	commits := "commits"
	if lag.Commits == 1 {
		commits = "commit"
	}
	return fmt.Sprintf("smith: warning: work branch %s is %d %s behind origin/%s: merge or rebase origin/%s into it\n", branch, lag.Commits, commits, lag.Base, lag.Base)
}

// settingsReport renders each resolved setting with where it came from,
// naming an unset model or effort as the agent's own default. The triage
// labels are reported only when the repo file sets one of them.
func settingsReport(s loop.Settings) string {
	var b strings.Builder
	for _, setting := range []struct {
		name  string
		value config.Value
	}{
		{"agent", s.Agent},
		{"model", s.Model},
		{"effort", s.Effort},
		{"push", s.Push},
	} {
		if setting.value.Value == "" {
			setting.value.Value = "the agent's own default"
		}
		fmt.Fprintf(&b, "%-7s %s\n", setting.name+":", setting.value)
	}
	labels := []struct {
		name  string
		value config.Value
	}{{"agent", s.AgentLabel}, {"human", s.HumanLabel}, {"spec", s.SpecLabel}}
	var named []string
	set := false
	for _, label := range labels {
		named = append(named, label.name+" "+label.value.String())
		set = set || label.value.Origin == config.FromRepoFile
	}
	if set {
		fmt.Fprintf(&b, "labels: %s\n", strings.Join(named, ", "))
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

// newLoopRunCmd builds `smith loop run <issue>`, which works a spec's tasks,
// or a single task, with a coding agent, or hands one task to an attached agent
// with --interactive. Each task is worked in a worktree of its own and landed
// on the execution's work branch, never in the checkout smith is run from. It
// exits 0 only when the execution is complete or the interactive run's task
// was closed.
func newLoopRunCmd(w loopWiring) *cobra.Command {
	limits := loop.DefaultLimits()
	var flags repofile.File
	var interactive, noPush bool
	var branch, base string
	cmd := &cobra.Command{
		Use:   "run <issue>",
		Short: "Work a spec's tasks, or a single task, to completion with a coding agent, on a work branch of its own",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if err := checkLimits(limits); err != nil {
				return reportInvalid(cmd, err)
			}
			if noPush {
				off := false
				flags.Push = &off
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
			pusher, err := w.origin(settings)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			github := tracker.NewGitHub(w.gh, settings.Labels())
			if err := warnOfLabels(cmd, github, settings); err != nil {
				return err
			}
			number, err := github.Local(cmd.Context(), ref)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			execution, err := github.Execution(cmd.Context(), number)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if err := loop.Admit(execution, settings.Labels()); err != nil {
				return reportInvalid(cmd, err)
			}
			bench, lag, err := w.workbench(cmd.Context(), execution, branch, base)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.ErrOrStderr(), lagWarning(bench.WorkBranch(), lag)); err != nil {
				return fmt.Errorf("write warning: %w", err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), settingsReport(settings)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			l := loop.Loop{
				Tracker:  github,
				Launcher: w.launcher,
				Bench:    bench,
				Pusher:   pusher,
				Agent:    adapter,
				Options:  settings.Options(),
				Prompt:   prompt,
				Limits:   limits,
				Progress: cmd.ErrOrStderr(),
				Hostname: w.hostname,
			}
			report, succeeded, err := runLoop(cmd.Context(), l, execution, interactive)
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
	cmd.Flags().StringVar(&branch, "branch", "", "the work branch, used exactly as named (default: the spec's or task's smith/<n>-<slug> branch)")
	cmd.Flags().StringVar(&base, "base", "", "the branch on origin a new work branch is cut from (default: origin's default branch)")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "push nothing after each closed task, over the repo file")
	return cmd
}

// runLoop works execution with l, unattended to completion or, when
// interactive, one task attached to the terminal, and returns the report of
// how it ended and whether it succeeded: the execution complete, or the
// interactive task closed.
func runLoop(ctx context.Context, l loop.Loop, execution tracker.Execution, interactive bool) (string, bool, error) {
	name := executionName(execution)
	number, _ := execution.Issue()
	if !interactive {
		outcome, err := l.Run(ctx, number)
		if err != nil {
			return "", false, fmt.Errorf("work %s: %w", name, err)
		}
		return runReport(name, l.Limits, outcome) + workBranchReport(outcome.WorkBranch), outcome.Complete(), nil
	}
	outcome, err := l.RunInteractive(ctx, number)
	if err != nil {
		return "", false, fmt.Errorf("work %s interactively: %w", name, err)
	}
	return interactiveReport(name, outcome) + workBranchReport(outcome.WorkBranch), outcome.Succeeded(), nil
}

// executionName names execution in a report as what it is: "task #80" for a
// task named on its own, "spec #42" otherwise.
func executionName(execution tracker.Execution) string {
	number, _ := execution.Issue()
	if execution.Single {
		return fmt.Sprintf("task #%d", number)
	}
	return fmt.Sprintf("spec #%d", number)
}

// workBranchReport renders the line naming the work branch a run's closed
// tasks land on.
func workBranchReport(branch string) string {
	return fmt.Sprintf("work branch: %s\n", branch)
}

// interactiveReport renders how an interactive run ended: whether the task it
// handed was closed or, with no task to hand, why not.
func interactiveReport(name string, outcome loop.InteractiveOutcome) string {
	switch {
	case !outcome.Handed:
		return runReport(name, loop.Limits{}, loop.Outcome{Remaining: outcome.Remaining})
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

// runReport renders how the run of the execution called name ended: complete,
// or why it stopped and what remains open, naming the tasks it skipped, the
// attempts each had and the task worktree each was left in.
func runReport(name string, limits loop.Limits, outcome loop.Outcome) string {
	if outcome.Complete() {
		return name + " complete\n"
	}
	var b strings.Builder
	switch {
	case outcome.Capped:
		fmt.Fprintf(&b, "%s stopped: reached the cap of %d agent runs\n", name, limits.MaxIterations)
	case len(outcome.Remaining.Skipped) > 0:
		fmt.Fprintf(&b, "%s stopped: the agent left a task open on every attempt\n", name)
	default:
		fmt.Fprintf(&b, "%s stopped: no task is available for an agent\n", name)
	}
	writeKinds(&b, outcome.Remaining, limits, false)
	for _, task := range outcome.Remaining.Skipped {
		fmt.Fprintf(&b, "  #%d left in %s\n", task.Number, outcome.Left[task.Number])
	}
	return b.String()
}

// newLoopListCmd builds `smith loop list <issue>`, which names the next
// available task of a spec, or of a single task, and sorts what remains, after
// the loop settings the repo file resolves to, running no agent and changing
// nothing in the tracker. Exit 0 once the issue is read, whatever it holds; an
// issue that cannot be read is reported naming why.
func newLoopListCmd(w loopWiring) *cobra.Command {
	return &cobra.Command{
		Use:   "list <issue>",
		Short: "Show the next task the loop would run, and what remains",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := tracker.ParseRef(args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			settings, err := w.settings(cmd.Context(), repofile.File{})
			if err != nil {
				return reportInvalid(cmd, err)
			}
			github := tracker.NewGitHub(w.gh, settings.Labels())
			number, err := github.Local(cmd.Context(), ref)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			execution, err := github.Execution(cmd.Context(), number)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), settingsReport(settings)+listReport(execution, loop.Survey(execution))); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
}

// listReport renders the execution, its next task, and one line for each kind
// of task that remains, leaving out the kinds it has none of.
func listReport(execution tracker.Execution, remaining loop.Remaining) string {
	var b strings.Builder
	_, title := execution.Issue()
	fmt.Fprintf(&b, "%s %s\n", executionName(execution), title)
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
				open = append(open, tracker.Ref{Repo: b.Repo, Number: b.Number}.String())
			}
		}
		if len(open) > 0 {
			ref += " (by " + strings.Join(open, ", ") + ")"
		}
		refs = append(refs, ref)
	}
	return strings.Join(refs, ", ")
}
