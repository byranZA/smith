package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
	"github.com/byranZA/smith/internal/workbench"
)

// sessionRun is a `loop run --repo` as the operator asked for it: the issue as
// they typed it, the declared repo to run in, the work branch and base they
// named, whether they asked for an interactive run, and the settings flags.
type sessionRun struct {
	issue       string
	repo        string
	branch      string
	base        string
	interactive bool
	flags       repofile.File
}

// startSession runs a loop run's preflight on this box in the foreground, then
// starts a loop-driven session in the repo's worktree for the work branch,
// whose root process is the same loop run without --repo, and reports how to
// watch it. Every refusal comes before anything is on disk, and a live session
// on the work branch is one.
func (w loopWiring) startSession(cmd *cobra.Command, ref tracker.Ref, run sessionRun) error {
	inv := invocation(cmd, "loop run", typed(cmd, []string{run.issue}))
	if run.interactive {
		return reportInvalid(cmd, fmt.Errorf("--interactive cannot run in a loop-driven session: start a human session with `%s` and run `smith loop run %s --interactive` in it",
			inv.Usage("session start", "--repo "+run.repo+" --branch <branch>"), run.issue))
	}
	box, err := w.box(inv)
	var absent *staging.AbsentError
	if errors.As(err, &absent) {
		return reportInvalid(cmd, fmt.Errorf("--repo names a repo on a box, and smith is not running on one: run `smith loop run %s` from inside the repo here", run.issue))
	}
	if err != nil {
		return reportInvalid(cmd, err)
	}
	env, err := sessionEnv(box, w.root, w.git, w.tmux, nil, inv)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	clone, err := env.Clone(run.repo)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	settings, err := w.committed(cmd, clone, cmp.Or(run.base, declaredBase(box.Repos, run.repo), "HEAD"), run.flags)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	admitted, err := w.admit(cmd, ref, settings, w.ghIn(clone))
	if err != nil {
		return err
	}
	number, title := admitted.execution.Issue()
	branch := cmp.Or(run.branch, workbench.Branch(number, title))
	if err := refuseLive(cmd, env, run.repo, branch); err != nil {
		return reportInvalid(cmd, err)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), settingsReport(settings)); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	started, err := session.Start(cmd.Context(), env, session.StartRequest{Repo: run.repo, Branch: branch, Base: run.base, Command: rootCommand(cmd, run.issue, branch)})
	if err != nil {
		return reportInvalid(cmd, err)
	}
	return writeLoopStarted(cmd, env.Hint, started)
}

// refuseLive refuses when the session for branch of repo is live, naming it
// and how to watch or stop it: one driver works a branch at a time.
func refuseLive(cmd *cobra.Command, env session.Env, repo, branch string) error {
	live, err := session.List(cmd.Context(), env, session.Filter{Repo: repo, State: session.LiveOnly})
	if err != nil {
		return fmt.Errorf("look for a live session on branch %s: %w", branch, err)
	}
	name := session.DeriveName(repo, branch)
	if !slices.ContainsFunc(live, func(s session.Session) bool { return s.Name == name }) {
		return nil
	}
	return fmt.Errorf("session %s is live on branch %s: watch it with `%s`, or stop it with `%s` and run again",
		name, branch, env.Hint.Command("session attach", name), env.Hint.Command("session stop", name))
}

// rootCommand is the loop run a loop-driven session's root process is: this
// box's smith running the issue with every flag the operator set but --repo,
// on exactly the work branch the session is for.
func rootCommand(cmd *cobra.Command, issue, branch string) []string {
	argv := []string{relay.BoxSmith, "loop", "run", issue}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed && f.Name != "repo" && f.Name != "branch" {
			argv = append(argv, "--"+f.Name+"="+f.Value.String())
		}
	})
	return append(argv, "--branch="+branch)
}

// writeLoopStarted reports the loop-driven session that was started and the
// commands, spelled for inv, to attach to it and to list the box's sessions.
func writeLoopStarted(cmd *cobra.Command, inv hint.Invocation, s session.Session) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "session %s is live on branch %s of %s, driven by the loop\nattach: %s\nlist:   %s\n",
		s.Name, s.Branch, s.Repo, inv.Command("session attach", s.Name), inv.Command("session list")); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// committed resolves the loop settings, flags over the repo file over the
// built-in defaults, and checks the loop prompt, as ref holds them in the git
// repo at clone, reading no checkout.
func (w loopWiring) committed(cmd *cobra.Command, clone, ref string, flags repofile.File) (loop.Settings, error) {
	commit, err := repofile.Committed(cmd.Context(), w.git, clone, ref)
	if err != nil {
		return loop.Settings{}, fmt.Errorf("resolve loop settings: %w", err)
	}
	settings, err := loop.ResolveSettings(flags, commit.File)
	if err != nil {
		return loop.Settings{}, fmt.Errorf("resolve loop settings: %w", err)
	}
	if !commit.Ejected {
		return settings, nil
	}
	if _, err := loop.ParsePrompt(ref+":.smith/prompt.md", commit.Prompt); err != nil {
		return loop.Settings{}, fmt.Errorf("load loop prompt: %w", err)
	}
	return settings, nil
}

// declaredBase is the base the blueprint declares for the named repo, empty
// when it declares none.
func declaredBase(repos []blueprint.Repo, name string) string {
	i := slices.IndexFunc(repos, func(r blueprint.Repo) bool { return r.Name == name })
	if i < 0 {
		return ""
	}
	return repos[i].Base
}

// dirExec launches commands from dir, the way gh finds the repo whose issues
// it reads when smith itself runs elsewhere.
type dirExec struct {
	dir string
}

// Run launches name with args from the directory, streaming the given
// stdin/stdout/stderr, and returns the process error.
func (d dirExec) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = d.dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s in %s: %w", name, d.dir, err)
	}
	return nil
}

// relayRun relays a loop run to box, as the on-box `loop run --repo` with
// every flag the operator set, streaming back its preflight output and exit
// status and never attaching. A run naming no repo, or asking to be
// interactive, is refused before anything travels, and a box refusing the
// relay for version skew is reacted to as for the session verbs.
func (w loopWiring) relayRun(cmd *cobra.Command, box string, run sessionRun) error {
	if run.interactive {
		return reportInvalid(cmd, fmt.Errorf("--interactive cannot run on a box: start a human session with `smith session start %s --repo %s --branch <branch>` and run `smith loop run %s --interactive` in it",
			box, cmp.Or(run.repo, "<repo>"), run.issue))
	}
	if run.repo == "" {
		return reportInvalid(cmd, fmt.Errorf("loop run on box %s needs --repo naming the repo on the box to run in", box))
	}
	target, err := relayTarget(w.home, box)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	verb := relay.Verb{Target: target, Box: box, Version: w.version, Args: append([]string{"loop", "run"}, typed(cmd, []string{run.issue})...)}
	send := func() error {
		return relay.Send(cmd.Context(), connection.New(verb.Target, w.ssh), verb, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	return reportRelay(cmd, w.skew.react(cmd, box, send))
}
