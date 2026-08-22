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
// tmux runners. connect is the exec boundary the connecting verbs cross to
// hand the operator's terminal to tmux. Placements are materialized from the box state directory at
// root — /etc/smith on a real box — which is passed in rather than reached for
// so a test drives the real command against a staged tree of its own.
//
// The verbs run against the local machine: on a provisioned box smith is on
// the operator's PATH, so an operator who has connected to the box gets the
// same surface a relay will later render for them from their laptop.
func newSessionCmd(resolve boxResolver, root string, git, tmux session.Runner, connect session.Execer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Work on a branch in its own worktree and tmux session",
	}
	cmd.AddCommand(newSessionStartCmd(resolve, root, git, tmux, connect))
	cmd.AddCommand(newSessionAttachCmd(resolve, root, git, tmux, connect))
	cmd.AddCommand(newSessionListCmd(resolve, root, git, tmux))
	cmd.AddCommand(newSessionStopCmd(resolve, root, git, tmux))
	return cmd
}

// newSessionListCmd builds `smith session list`. It enumerates the sessions on
// the box and prints them under the four columns the work-state question is
// answered in.
//
// It exits zero whatever it finds: a non-zero exit on "something is unpushed"
// would conflate the command failing with the data having a property, and the
// listing exists to be read before a teardown the operator does by hand.
func newSessionListCmd(resolve boxResolver, root string, git, tmux session.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the sessions on this box and whether they are running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolve()
			if err != nil {
				return err
			}
			env, err := sessionEnv(resolved, root, git, tmux, nil)
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
// `smith session start --repo <name> --branch <name> [--base <ref>]
// [--detach]`. It cuts the branch from --base, else from the base the
// blueprint declares for the repo, else from the repo's default branch,
// creates a worktree for it below the repo's worktrees directory, and launches
// a tmux session in that worktree.
//
// It then puts the operator in that session writable, because start is the
// verb of someone who is there to work. --detach opts out of that half and
// returns instead, which is what a caller with no human present drives.
func newSessionStartCmd(resolve boxResolver, root string, git, tmux session.Runner, connect session.Execer) *cobra.Command {
	var repo, branch, base string
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
			env, err := sessionEnv(resolved, root, git, tmux, connect)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			started, err := session.Start(cmd.Context(), env, session.StartRequest{Repo: repo, Branch: branch, Base: base})
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if detach {
				return writeStarted(cmd, started)
			}
			if err := session.Attach(cmd.Context(), env, started.Name, session.Interact); err != nil {
				return reportInvalid(cmd, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "the declared repo to cut the worktree from")
	cmd.Flags().StringVar(&branch, "branch", "", "the branch to work on, cut from the base when it is new")
	cmd.Flags().StringVar(&base, "base", "", "the ref a new branch is cut from; refused against a branch that already exists")
	cmd.Flags().BoolVar(&detach, "detach", false, "stand the session up without connecting to it")
	return cmd
}

// newSessionAttachCmd builds `smith session attach <name> [--interact]`. It
// hands the operator's terminal to the session's tmux session — read-only
// unless --interact is given, because an operator attaching may be there to
// watch an agent work rather than to type into its pane.
//
// smith execs into tmux, so this command does not return: what the operator
// sees afterwards is tmux itself, and disconnecting leaves the session
// running.
func newSessionAttachCmd(resolve boxResolver, root string, git, tmux session.Runner, connect session.Execer) *cobra.Command {
	var interact bool
	cmd := &cobra.Command{
		Use:   "attach <name>",
		Short: "Connect a terminal to a session, read-only unless asked to interact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolve()
			if err != nil {
				return err
			}
			env, err := sessionEnv(resolved, root, git, tmux, connect)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			mode := session.Observe
			if interact {
				mode = session.Interact
			}
			if err := session.Attach(cmd.Context(), env, args[0], mode); err != nil {
				return reportInvalid(cmd, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&interact, "interact", false, "connect writable rather than read-only")
	return cmd
}

// newSessionStopCmd builds `smith session stop <name>`. It ends the session's
// tmux session and leaves its worktree and its branch exactly where they are.
//
// It takes no confirmation and has no --force: nothing it does loses work, so
// a gate here would only teach the operator to wave one away at the verb that
// is safe, and mean it at the one that is not.
func newSessionStopCmd(resolve boxResolver, root string, git, tmux session.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "End a session's tmux session, keeping its worktree and branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolve()
			if err != nil {
				return err
			}
			env, err := sessionEnv(resolved, root, git, tmux, nil)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			name := args[0]
			if err := session.Stop(cmd.Context(), env, name); err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "session %s is stopped; its worktree and branch are untouched\n", name); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
}

// sessionEnv turns the box's resolved configuration into what a session verb
// runs against: an absolute workspace root and the repos the blueprint
// declares, paired with the commands smith drives the box with.
func sessionEnv(resolved config.Resolved, root string, git, tmux session.Runner, connect session.Execer) (session.Env, error) {
	workspace, err := boxPath(resolved.Workspace.Value)
	if err != nil {
		return session.Env{}, err
	}
	return session.Env{
		Workspace: workspace,
		Repos:     declaredRepos(resolved.Repos),
		Git:       git,
		Tmux:      tmux,
		Exec:      connect,
		Placer:    stagedPlacer{root: root, repos: resolved.Repos},
	}, nil
}

// stagedPlacer materializes a repo's declared placements into a worktree from
// the bytes `machine setup` staged on this box. It is the seam between what
// the blueprint declares and where the bytes live: internal/session decides
// when a placement converges, internal/staging owns how, and nothing on the
// box resolves a source reference.
type stagedPlacer struct {
	// root is the box state directory the bytes were staged under.
	root string
	// repos are the repos the box's blueprint declares, carrying the
	// placements each one asks for in its worktrees.
	repos []blueprint.Repo
}

// Place converges the named repo's declared placements into the worktree at
// dir. A repo the blueprint does not declare places nothing, which is the case
// the session verbs have already refused before reaching here.
func (p stagedPlacer) Place(repo, dir string) error {
	for _, r := range p.repos {
		if r.Name != repo {
			continue
		}
		if _, err := staging.Place(p.root, repo, dir, r.Placements); err != nil {
			return fmt.Errorf("place the files repo %q declares into %s: %w", repo, dir, err)
		}
		return nil
	}
	return nil
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
