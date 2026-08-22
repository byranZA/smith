package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/relay"
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

// newSessionCmd builds `smith session` and its subcommands from the wiring
// they run through, which carries both what a verb needs here and what it
// needs to reach a box.
//
// Every verb takes an optional leading box, and that argument is the whole of
// the rule: naming one relays the verb to the smith installed on that box,
// naming none runs it here. So `smith session list dev` on the operator's
// laptop and `smith session list` after SSHing in are one implementation of
// the verb, reached through one door.
func newSessionCmd(w sessionWiring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Work on a branch in its own worktree and tmux session",
	}
	cmd.AddCommand(newSessionStartCmd(w))
	cmd.AddCommand(newSessionAttachCmd(w))
	cmd.AddCommand(newSessionListCmd(w))
	cmd.AddCommand(newSessionStopCmd(w))
	cmd.AddCommand(newSessionRemoveCmd(w))
	return cmd
}

// sessionWiring is everything a session verb is built from: what it needs to
// run here, and what it needs to relay. Both halves are always present,
// because which one a verb uses is decided by the operator's own command line
// and not at wiring time.
type sessionWiring struct {
	// box reads the blueprint staged on this box, for a verb running here.
	box boxResolver
	// home locates the operator's config home, where a box name is resolved
	// to the target it was proven at.
	home homeResolver
	// root is the box state directory the placement bytes were staged under.
	root string
	// git and tmux are the commands a verb running here drives the box with.
	git, tmux session.Runner
	// connect replaces smith's own process: with tmux for a verb running
	// here, with ssh for one that relays.
	connect session.Execer
	// ssh launches the local ssh binary a relayed verb travels over.
	ssh connection.Exec
	// version is this smith's version, which every relayed invocation carries
	// so the box can refuse a command line it may not mean the same thing by.
	version string
}

// leading splits a verb's positional arguments into the box it names and the
// arguments the verb itself takes. takes is how many of those there are, so an
// argument beyond them can only be the box — which is the whole of the rule
// for every verb whose arity is fixed.
func leading(args []string, takes int) (string, []string) {
	if len(args) > takes {
		return args[0], args[1:]
	}
	return "", args
}

// leadingBox splits a batch removal's arguments into the box it names and the
// sessions it is to remove. rm is the one verb whose leading argument is
// ambiguous, because it takes any number of names, so the argument is read as
// a box only when it is one the operator registered or wrote out in full. On
// the box, where a batch is composed from `session list --names`, there is no
// inventory to hit and a session name never carries an "@".
func (w sessionWiring) leadingBox(args []string) (string, []string, error) {
	if len(args) < 2 {
		return "", args, nil
	}
	inv, err := lookupInventory(w.home, args[0])
	if err != nil {
		return "", nil, err
	}
	if _, registered := inventory.Lookup(inv, args[0]); registered || strings.Contains(args[0], "@") {
		return args[0], args[1:], nil
	}
	return "", args, nil
}

// verb renders the invocation the box is to run: the session verb by name, the
// arguments it takes, and the flags the operator actually set, spelled back as
// --flag=value so the box parses the command line they typed. The box target
// is resolved here under the shared rule — an "@" is a literal target, a bare
// value is looked up in the inventory — and an empty one is the verb running
// on this machine.
func (w sessionWiring) verb(cmd *cobra.Command, name, box string, args ...string) (relay.Verb, error) {
	target, err := w.target(box)
	if err != nil {
		return relay.Verb{}, err
	}
	relayed := append([]string{"session", name}, args...)
	cmd.Flags().Visit(func(f *pflag.Flag) {
		relayed = append(relayed, "--"+f.Name+"="+f.Value.String())
	})
	return relay.Verb{Target: target, Version: w.version, Args: relayed}, nil
}

// target resolves the box a verb named into the ssh target it relays to.
func (w sessionWiring) target(box string) (string, error) {
	return relayTarget(w.home, box)
}

// localEnv resolves what a verb running on this machine acts against: the
// blueprint staged here, paired with the commands smith drives the box with.
// A staged blueprint that is absent or cannot be trusted travels out untouched
// so it keeps the exit code its kind is owed.
func (w sessionWiring) localEnv(cmd *cobra.Command, connect session.Execer) (session.Env, error) {
	resolved, err := w.box()
	if err != nil {
		return session.Env{}, err
	}
	env, err := sessionEnv(resolved, w.root, w.git, w.tmux, connect)
	if err != nil {
		return session.Env{}, reportInvalid(cmd, err)
	}
	return env, nil
}

// reportRelay maps a relayed verb's outcome onto the exit the operator gets. A
// box that ran smith and refused — the version-skew refusal above all — has
// already said why on the operator's terminal, so its exit code is carried out
// with nothing added; a box with no smith and a box that could not be reached
// are reported here. Anything else is the verb having run on this machine and
// having reported itself, exactly as it did before there was a relay.
func reportRelay(cmd *cobra.Command, err error) error {
	var exit *relay.ExitError
	if errors.As(err, &exit) {
		return &exitError{code: exit.Code}
	}
	var absent *relay.NotInstalledError
	if errors.As(err, &absent) {
		return reportInvalid(cmd, absent)
	}
	if errors.Is(err, connection.ErrConnect) {
		return reportInvalid(cmd, err)
	}
	return err
}

// sessionVerb is what a session subcommand hands the dispatcher: the name it
// travels to the box as, how many arguments of its own it takes, whether it
// hands the operator's terminal over, and what it does when it runs here.
type sessionVerb struct {
	// name is the subcommand the box is handed the verb back as.
	name string
	// takes is how many positional arguments the verb itself takes, so an
	// argument beyond them can only be the box.
	takes int
	// batch marks the verb that takes any number of names, whose leading
	// argument is read as a box only when it is one.
	batch bool
	// terminal marks a verb that may hand the box's tmux the operator's
	// terminal, which is what its local implementation is given an execer
	// for.
	terminal bool
	// connects marks a verb handing that terminal over on this invocation, so
	// it travels by replacing smith with ssh rather than by streaming.
	connects bool
	// local is what the verb does when it runs on this machine: the arguments
	// left after the box was taken off the front, against the box the
	// blueprint staged here describes.
	local func(args []string, env session.Env) error
}

// dispatch runs a session verb under the one rule all five share. The leading
// argument names the box or is the verb's own; a named box relays the command
// line the operator typed to the smith installed there; no box named runs the
// verb here, against the blueprint staged on this machine. A verb handing the
// terminal over replaces smith with ssh, and every other one streams the box's
// output back and carries its exit out.
func (w sessionWiring) dispatch(cmd *cobra.Command, args []string, v sessionVerb) error {
	box, rest, err := w.splitBox(args, v)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	verb, err := w.verb(cmd, v.name, box, rest...)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	local := func() error {
		var connect session.Execer
		if v.terminal {
			connect = w.connect
		}
		env, err := w.localEnv(cmd, connect)
		if err != nil {
			return err
		}
		return v.local(rest, env)
	}
	if v.connects {
		return reportRelay(cmd, relay.Connect(w.connect, verb, local))
	}
	return reportRelay(cmd, relay.Run(cmd.Context(), w.ssh, verb, local, cmd.OutOrStdout(), cmd.ErrOrStderr()))
}

// splitBox takes the box off the front of a verb's positional arguments,
// under the rule the verb's own arity gives it.
func (w sessionWiring) splitBox(args []string, v sessionVerb) (string, []string, error) {
	if v.batch {
		return w.leadingBox(args)
	}
	box, rest := leading(args, v.takes)
	return box, rest, nil
}

// newSessionListCmd builds
// `smith session list [--repo <name>] [--live|--stopped] [--names]`. It
// enumerates the sessions on the box and prints them under the four columns
// the work-state question is answered in.
//
// Scale is answered by the filters rather than by the layout: the table stays
// flat and ungrouped at fifty rows, and the operator narrows it. --names drops
// the table entirely and prints one bare name per line, which is what a batch
// removal is composed from.
//
// It exits zero whatever it finds: a non-zero exit on "something is unpushed"
// would conflate the command failing with the data having a property, and the
// listing exists to be read before a teardown the operator does by hand.
func newSessionListCmd(w sessionWiring) *cobra.Command {
	var repo string
	var live, stopped, names bool
	cmd := &cobra.Command{
		Use:   "list [<box>]",
		Short: "List the sessions on a box and whether they are running",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return w.dispatch(cmd, args, sessionVerb{name: "list", local: func(_ []string, env session.Env) error {
				filter, err := sessionFilter(repo, live, stopped)
				if err != nil {
					return reportInvalid(cmd, err)
				}
				sessions, err := session.List(cmd.Context(), env, filter)
				if err != nil {
					return reportInvalid(cmd, err)
				}
				render := session.Readout
				if names {
					render = session.Names
				}
				if _, err := fmt.Fprint(cmd.OutOrStdout(), render(sessions)); err != nil {
					return fmt.Errorf("write session listing: %w", err)
				}
				return nil
			}})
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "list only the sessions of one declared repo")
	cmd.Flags().BoolVar(&live, "live", false, "list only the sessions that are running")
	cmd.Flags().BoolVar(&stopped, "stopped", false, "list only the sessions that are not running")
	cmd.Flags().BoolVar(&names, "names", false, "print one bare session name per line, with no table and no summary")
	return cmd
}

// sessionFilter turns the listing flags into the filter the package takes.
// --live and --stopped are opposites, so asking for both is refused rather
// than answered with an empty table, which reads exactly like a box that has
// no sessions.
func sessionFilter(repo string, live, stopped bool) (session.Filter, error) {
	filter := session.Filter{Repo: repo}
	switch {
	case live && stopped:
		return session.Filter{}, errors.New("--live and --stopped are opposites: ask for one of them, or for neither to list both")
	case live:
		filter.State = session.LiveOnly
	case stopped:
		filter.State = session.StoppedOnly
	}
	return filter, nil
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
func newSessionStartCmd(w sessionWiring) *cobra.Command {
	var repo, branch, base string
	var detach bool
	cmd := &cobra.Command{
		Use:   "start [<box>]",
		Short: "Stand up a worktree and a tmux session for a branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A start that will connect hands the terminal to the box's tmux,
			// so it travels the same way attach does; one that detaches is an
			// ordinary command whose report the operator reads.
			return w.dispatch(cmd, args, sessionVerb{name: "start", terminal: true, connects: !detach, local: func(_ []string, env session.Env) error {
				if repo == "" || branch == "" {
					return reportInvalid(cmd, errors.New("session start needs --repo naming a declared repo and --branch naming the branch to work on"))
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
			}})
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
// watch an agent work rather than to type into its session.
//
// smith execs into tmux, so this command does not return: what the operator
// sees afterwards is tmux itself, and disconnecting leaves the session
// running.
func newSessionAttachCmd(w sessionWiring) *cobra.Command {
	var interact bool
	cmd := &cobra.Command{
		Use:   "attach [<box>] <name>",
		Short: "Connect a terminal to a session, read-only unless asked to interact",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return w.dispatch(cmd, args, sessionVerb{name: "attach", takes: 1, terminal: true, connects: true, local: func(names []string, env session.Env) error {
				mode := session.Observe
				if interact {
					mode = session.Interact
				}
				if err := session.Attach(cmd.Context(), env, names[0], mode); err != nil {
					return reportInvalid(cmd, err)
				}
				return nil
			}})
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
func newSessionStopCmd(w sessionWiring) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [<box>] <name>",
		Short: "End a session's tmux session, keeping its worktree and branch",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return w.dispatch(cmd, args, sessionVerb{name: "stop", takes: 1, local: func(names []string, env session.Env) error {
				name := names[0]
				if err := session.Stop(cmd.Context(), env, name); err != nil {
					return reportInvalid(cmd, err)
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "session %s is stopped; its worktree and branch are untouched\n", name); err != nil {
					return fmt.Errorf("write report: %w", err)
				}
				return nil
			}})
		},
	}
}

// newSessionRemoveCmd builds `smith session rm <name>... [--force]`. It
// reclaims the named sessions' worktrees and keeps their branches, refusing on
// a dirty worktree or a live session and on nothing else.
//
// It takes names and no filters of its own. A --stopped here would make the
// target set of a delete implicit, so cobra rejects it as the unknown flag it
// is; the listing is where filters live, and its --names output is what a
// batch is composed from.
//
// The batch is all-or-nothing: one offender refuses the lot, every offender is
// named, and nothing is removed. It never prompts, with a terminal or without:
// a refusal prints what would be lost and the exact --force command that
// overrides it, and exits non-zero, so a human and a loop are answered
// identically.
func newSessionRemoveCmd(w sessionWiring) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm [<box>] <name>...",
		Short: "Reclaim the worktrees of one or more sessions, keeping their branches",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return w.dispatch(cmd, args, sessionVerb{name: "rm", batch: true, local: func(names []string, env session.Env) error {
				removed, err := session.Remove(cmd.Context(), env, names, force)
				if err != nil {
					return reportInvalid(cmd, err)
				}
				return writeRemoved(cmd, removed)
			}})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "reclaim the worktrees even if a session is running or its worktree is dirty")
	return cmd
}

// writeRemoved reports what the removal cost and what it kept, one line per
// session: the branch always survives, and the commits on it that no remote
// has are named so the operator learns of them here rather than the next time
// they look for the work.
func writeRemoved(cmd *cobra.Command, removals []session.Removal) error {
	for _, removed := range removals {
		if err := writeOneRemoved(cmd, removed); err != nil {
			return err
		}
	}
	return nil
}

// writeOneRemoved reports a single session's removal.
func writeOneRemoved(cmd *cobra.Command, removed session.Removal) error {
	report := fmt.Sprintf("session %s removed", removed.Name)
	if removed.Killed {
		report += ", its tmux session killed"
	}
	report += fmt.Sprintf("; branch %s kept", removed.Branch)
	switch removed.Unpushed {
	case 0:
	case 1:
		report += ", 1 commit not on any remote"
	default:
		report += fmt.Sprintf(", %d commits not on any remote", removed.Unpushed)
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
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
