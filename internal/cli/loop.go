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
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
	"github.com/byranZA/smith/internal/workbench"
)

// loopWiring is what the loop verbs reach the outside world through: gh for
// the tracker, the launcher that starts an agent, the bench that sets up an
// execution's work branch and task worktrees in the repo a directory is in
// and finds how far an adopted one is behind its base, the pusher that pushes
// the work branch from a directory, the PATH search that finds an agent here, the
// hostname that names this machine on a skipped task, and git, the config home
// and the working directory that locate the repo file. On a box it also reaches
// gh run in a repo's clone, the configuration the box was built from, the
// state directory its placements were staged under, tmux, and the mise that
// finds an agent for a session. To relay a run to
// a box it reaches ssh, declares this smith's version, and reacts to a box that
// refuses it for version skew.
type loopWiring struct {
	gh       tracker.Runner
	ghIn     func(dir string) tracker.Runner
	box      boxResolver
	root     string
	tmux     session.Runner
	launcher loop.Launcher
	bench    func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error)
	pusher   func(dir string) loop.Pusher
	lookPath func(name string) (string, error)
	mise     session.Runner
	hostname func() (string, error)
	git      repofile.Runner
	home     homeResolver
	workdir  workdirResolver
	ssh      connection.Exec
	version  string
	skew     skew
}

// systemLoop is the loop wiring of the operator's own machine: the real gh,
// agents started as processes on smith's own terminal, the real git keeping
// the workbench and pushing to origin, the real PATH and the real hostname.
func systemLoop() loopWiring {
	return loopWiring{
		gh:       connection.System(),
		ghIn:     func(dir string) tracker.Runner { return dirExec{dir: dir} },
		box:      stagedBoxConfig,
		root:     staging.Root,
		tmux:     connection.System(),
		launcher: loop.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
		bench:    systemBench,
		pusher:   func(dir string) loop.Pusher { return loop.Origin{Git: connection.System(), Dir: dir} },
		lookPath: exec.LookPath,
		mise:     connection.System(),
		hostname: os.Hostname,
		git:      connection.System(),
		home:     userConfigHome,
		workdir:  currentDir,
		ssh:      connection.System(),
		version:  resolveVersion(),
		skew:     operatorSkew(),
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
// single task, in the repo smith is run from, reading the tracker through gh.
// Each runs where smith is invoked; on a box, `loop run --repo` starts the run
// in a loop-driven session instead, and `loop run <box>` relays it there.
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
// was closed. On a box, --repo starts the run in a loop-driven session instead
// and returns once it is live; a leading box relays the run to that box.
func newLoopRunCmd(w loopWiring) *cobra.Command {
	limits := loop.DefaultLimits()
	var flags repofile.File
	var interactive, noPush, inSession bool
	var branch, base, repo string
	cmd := &cobra.Command{
		Use:   "run [<box>] <issue>",
		Short: "Work a spec's tasks, or a single task, to completion with a coding agent, on a work branch of its own",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			box, args := leading(args, 1)
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
			if box != "" {
				return w.relayRun(cmd, box, sessionRun{issue: args[0], repo: repo, interactive: interactive})
			}
			if repo != "" {
				return w.startSession(cmd, ref, sessionRun{issue: args[0], repo: repo, branch: branch, base: base, interactive: interactive, flags: flags})
			}
			settings, err := w.settings(cmd.Context(), flags)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			prompt, err := w.prompt(cmd.Context())
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if err := installed(cmd, settings.Agent.Value, w.lookPath); err != nil {
				return err
			}
			admitted, err := w.admit(cmd, ref, settings, w.gh)
			if err != nil {
				return err
			}
			adapter, github, execution := admitted.adapter, admitted.github, admitted.execution
			pusher, err := w.origin(settings)
			if err != nil {
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
			recorder, err := w.recorder(cmd.Context(), inSession)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			l := loop.Loop{
				Recorder: recorder,
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
	cmd.Flags().StringVar(&repo, "repo", "", "on a box, the repo to start a loop-driven session in, rather than running here")
	cmd.Flags().BoolVar(&inSession, inSessionFlag, false, "the run is a loop-driven session's root process, recording how it ends in the session record")
	if err := cmd.Flags().MarkHidden(inSessionFlag); err != nil {
		panic(fmt.Sprintf("hide --%s: %v", inSessionFlag, err))
	}
	return cmd
}

// admission is what a loop run's preflight found: the agent adapter each task
// is handed to, the tracker, and the execution it admitted.
type admission struct {
	adapter   agent.Adapter
	github    tracker.GitHub
	execution tracker.Execution
}

// installed refuses, reported on cmd, an agent that lookPath cannot find, so
// a run fails before it starts rather than on its first task.
func installed(cmd *cobra.Command, name string, lookPath func(name string) (string, error)) error {
	if _, err := lookPath(name); err != nil {
		return reportInvalid(cmd, fmt.Errorf("agent %s is not installed: %w", name, err))
	}
	return nil
}

// admit runs the preflight a loop run shares wherever it runs: it looks the
// agent settings name up, warns of triage labels the repo lacks, and reads and
// admits the execution ref names through gh. Each refusal is already reported
// on cmd. Whether the agent is installed is the caller's to check, with
// installed, against the PATH the run will have.
func (w loopWiring) admit(cmd *cobra.Command, ref tracker.Ref, settings loop.Settings, gh tracker.Runner) (admission, error) {
	adapter, err := agent.Lookup(settings.Agent.Value)
	if err != nil {
		return admission{}, reportInvalid(cmd, err)
	}
	github := tracker.NewGitHub(gh, settings.Labels())
	if err := warnOfLabels(cmd, github, settings); err != nil {
		return admission{}, err
	}
	number, err := github.Local(cmd.Context(), ref)
	if err != nil {
		return admission{}, reportInvalid(cmd, err)
	}
	execution, err := github.Execution(cmd.Context(), number)
	if err != nil {
		return admission{}, reportInvalid(cmd, err)
	}
	if err := loop.Admit(execution, settings.Labels()); err != nil {
		return admission{}, reportInvalid(cmd, err)
	}
	return admission{adapter: adapter, github: github, execution: execution}, nil
}

// runLoop works execution with l, unattended to completion or, when
// interactive, one task attached to the terminal, and returns the report of
// how it ended and whether it succeeded: the execution complete, or the
// interactive task closed.
func runLoop(ctx context.Context, l loop.Loop, execution tracker.Execution, interactive bool) (string, bool, error) {
	name := loop.Name(execution)
	number, _ := execution.Issue()
	if !interactive {
		outcome, err := l.Run(ctx, number)
		if err != nil {
			return "", false, fmt.Errorf("work %s: %w", name, err)
		}
		return outcome.Report(name, l.Limits) + loop.WorkBranchReport(outcome.WorkBranch), outcome.Complete(), nil
	}
	outcome, err := l.RunInteractive(ctx, number)
	if err != nil {
		return "", false, fmt.Errorf("work %s interactively: %w", name, err)
	}
	return interactiveReport(name, outcome) + loop.WorkBranchReport(outcome.WorkBranch), outcome.Succeeded(), nil
}

// interactiveReport renders how an interactive run ended: whether the task it
// handed was closed or, with no task to hand, why not.
func interactiveReport(name string, outcome loop.InteractiveOutcome) string {
	switch {
	case !outcome.Handed:
		return loop.Outcome{Remaining: outcome.Remaining}.Report(name, loop.Limits{})
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
	fmt.Fprintf(&b, "%s %s\n", loop.Name(execution), title)
	switch next, ok := remaining.Next(); {
	case ok:
		fmt.Fprintf(&b, "next: #%d %s\n", next.Number, next.Title)
	case remaining.Complete():
		b.WriteString("next: none, spec complete\n")
	default:
		b.WriteString("next: none\n")
	}
	b.WriteString(loop.Kinds(remaining, loop.Limits{}, true))
	return b.String()
}
