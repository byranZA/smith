package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
)

// boxResolver reads the configuration the box smith is running on was built
// from. It is passed into the session command rather than called inside it, so
// a test drives the real command against a temp workspace.
type boxResolver func() (config.Resolved, error)

// stagedBoxConfig reads what `machine setup` staged on this box and resolves
// it the same way the operator's own `blueprint check` does, so the workspace
// root and the declared repos a session verb acts on are the ones the operator
// would be shown. A box with nothing staged, or a staged document that cannot
// be trusted, is refused with the exit code its kind is owed.
//
// The operator's preferences are not consulted: they live on the operator's
// machine, and a box is deliberately given no config home of its own.
func stagedBoxConfig() (config.Resolved, error) {
	b, err := staging.Load(staging.Root)
	if err != nil {
		return config.Resolved{}, fmt.Errorf("read the blueprint staged on this box: %w", err)
	}
	return config.Resolve(config.Overrides{}, &b, nil), nil
}

// newSessionCmd builds `smith session` and its subcommands, reading the box's
// staged configuration through resolve and driving the box through the git and
// tmux runners.
//
// The verbs run against the local machine: on a provisioned box smith is on
// the operator's PATH, so an operator who has connected to the box gets the
// same surface a relay will later render for them from their laptop.
func newSessionCmd(resolve boxResolver, git, tmux session.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Work on a branch in its own worktree and tmux session",
	}
	cmd.AddCommand(newSessionStartCmd(resolve, git, tmux))
	cmd.AddCommand(newSessionListCmd(resolve, git, tmux))
	return cmd
}

// newSessionListCmd builds `smith session list`. It enumerates the sessions on
// the box and prints them under the four columns the work-state question is
// answered in.
//
// It exits zero whatever it finds: a non-zero exit on "something is unpushed"
// would conflate the command failing with the data having a property, and the
// listing exists to be read before a teardown the operator does by hand.
func newSessionListCmd(resolve boxResolver, git, tmux session.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the sessions on this box and whether they are running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolve()
			if err != nil {
				return err
			}
			env, err := sessionEnv(resolved, git, tmux)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			sessions, err := session.List(cmd.Context(), env, session.Filter{})
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), session.Readout(sessions)); err != nil {
				return fmt.Errorf("write session listing: %w", err)
			}
			return nil
		},
	}
}

// newSessionStartCmd builds
// `smith session start --repo <name> --branch <name> [--detach]`. It cuts the
// branch from the repo's default branch, creates a worktree for it below the
// repo's worktrees directory, and launches a tmux session in that worktree.
//
// It stands the session up and returns without connecting to it, which is what
// --detach asks for and, for now, all start does: connecting is a later slice,
// and a verb that always detaches is the one a caller with no human present
// can drive.
func newSessionStartCmd(resolve boxResolver, git, tmux session.Runner) *cobra.Command {
	var repo, branch string
	var detach bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Stand up a worktree and a tmux session for a branch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if repo == "" || branch == "" {
				return reportInvalid(cmd, errors.New("session start needs --repo naming a declared repo and --branch naming the branch to work on"))
			}
			resolved, err := resolve()
			if err != nil {
				return err
			}
			env, err := sessionEnv(resolved, git, tmux)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			started, err := session.Start(cmd.Context(), env, session.StartRequest{Repo: repo, Branch: branch})
			if err != nil {
				return reportInvalid(cmd, err)
			}
			return writeStarted(cmd, started)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "the declared repo to cut the worktree from")
	cmd.Flags().StringVar(&branch, "branch", "", "the branch to work on, cut from the repo's default branch when it is new")
	cmd.Flags().BoolVar(&detach, "detach", false, "stand the session up without connecting to it, which is what start does either way for now")
	return cmd
}

// sessionEnv turns the box's resolved configuration into what a session verb
// runs against: an absolute workspace root and the repos the blueprint
// declares, paired with the commands smith drives the box with.
func sessionEnv(resolved config.Resolved, git, tmux session.Runner) (session.Env, error) {
	workspace, err := boxPath(resolved.Workspace.Value)
	if err != nil {
		return session.Env{}, err
	}
	return session.Env{
		Workspace: workspace,
		Repos:     declaredRepos(resolved.Repos),
		Git:       git,
		Tmux:      tmux,
	}, nil
}

// declaredRepos narrows the blueprint's repos to what a session needs of them:
// the workspace directory each lives in, the branch its worktrees start from,
// and the paths it places files at — which the work-state predicate subtracts
// so a file smith wrote is not read as the operator's work.
func declaredRepos(repos []blueprint.Repo) []session.Repo {
	out := make([]session.Repo, len(repos))
	for i, r := range repos {
		out[i] = session.Repo{Name: r.Name, Base: r.Base, Placements: placedAt(r.Placements)}
	}
	return out
}

// placedAt narrows a repo's placements to the destination paths, which is all
// the dirty check needs of them: resolving a from: reference belongs to the
// stages that stage the bytes.
func placedAt(placements []blueprint.Placement) []string {
	if len(placements) == 0 {
		return nil
	}
	to := make([]string, len(placements))
	for i, p := range placements {
		to[i] = p.To
	}
	return to
}

// boxPath expands a workspace root written the way an operator writes it —
// ~/workspace — into the absolute path the verbs act on. The expansion happens
// here rather than in internal/session, which is handed paths and never
// resolves one.
func boxPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory to expand %s: %w", path, err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// writeStarted reports the session that was stood up: the name every other
// session verb takes, and the branch it is on.
func writeStarted(cmd *cobra.Command, s session.Session) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "session %s is live on branch %s of %s\n", s.Name, s.Branch, s.Repo); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
