package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/workspace"
)

// stagedResolver reads the blueprint the box smith is running on was built
// from. It is passed into the workspace command rather than called inside it,
// so a test drives the real command against a document of its own.
type stagedResolver func() (blueprint.Blueprint, error)

// stagedBlueprint reads what `machine setup` staged on this box. A box with
// nothing staged, or a staged document that cannot be trusted, travels out
// untouched so it keeps the exit code its kind is owed — the refusals are
// inherited from the staged-config contract, never redefined here.
func stagedBlueprint() (blueprint.Blueprint, error) {
	b, err := staging.Load(staging.Root)
	if err != nil {
		return blueprint.Blueprint{}, fmt.Errorf("read the blueprint staged on this box: %w", err)
	}
	return b, nil
}

// pathResolver locates a directory on the box a verb acts against. It is
// passed into the workspace command rather than read inside it, so a test
// drives the real command against a directory of its own.
type pathResolver func() (string, error)

// boxHomeDir locates the smith user's home on this box, which is where a
// ~/-relative placement destination lands.
func boxHomeDir() (string, error) { return boxPath("~") }

// workspaceWiring is everything the workspace verbs are built from: what they
// need to run on a box, and what they need to relay to one. Both halves are
// always present, because which one is used is decided by the operator's own
// command line and not at wiring time.
type workspaceWiring struct {
	// blueprint reads the blueprint staged on this box, for a verb running
	// here.
	blueprint stagedResolver
	// home locates the operator's config home, where a box name is resolved
	// to the target it was proven at.
	home homeResolver
	// command launches the commands the stage converges the box with.
	command workspace.Runner
	// root is the box state directory `machine setup` staged the blueprint
	// and its placement bytes under.
	root string
	// boxHome locates the smith user's home on this box, which the
	// blueprint's ~/-relative paths resolve against.
	boxHome pathResolver
	// ssh launches the local ssh binary a relayed verb travels over.
	ssh connection.Exec
	// version is this smith's version, which every relayed invocation carries
	// so the box can refuse a command line it may not mean the same thing by.
	version string
}

// newWorkspaceCmd builds `smith workspace` and its subcommands.
func newWorkspaceCmd(w workspaceWiring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Converge a box to the workspace its blueprint declares",
	}
	cmd.AddCommand(newWorkspaceConvergeCmd(w))
	return cmd
}

// newWorkspaceConvergeCmd builds `smith workspace converge [<box>]`. It reads
// the blueprint staged on the box and converges the box to the workspace it
// declares, streaming each step as it finishes.
//
// It is the last stage of `machine setup`, which relays it to the box it has
// just provisioned, and it is a real verb of its own so an operator can
// re-converge a workspace without re-running the whole pipeline. The leading
// argument is the same rule every relayed verb shares: naming a box sends the
// verb to the smith installed there, naming none runs it here.
//
// It never deletes: a step it cannot converge is reported and the run exits
// non-zero, and what the box already holds is left where it is.
func newWorkspaceConvergeCmd(w workspaceWiring) *cobra.Command {
	return &cobra.Command{
		Use:   "converge [<box>]",
		Short: "Bring a box's workspace up to what its blueprint declares",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			box, _ := leading(args, 0)
			target, err := relayTarget(w.home, box)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			verb := relay.Verb{Target: target, Version: w.version, Args: []string{"workspace", "converge"}}
			local := func() error { return w.converge(cmd) }
			return reportRelay(cmd, relay.Run(cmd.Context(), w.ssh, verb, local, cmd.OutOrStdout(), cmd.ErrOrStderr()))
		},
	}
}

// converge runs the stage on this box: read what was staged, plan the ordered
// units of work, and apply them, streaming progress to the operator's terminal
// as each step finishes. The final summary follows it, and a run with a failed
// step exits non-zero having already said which one.
func (w workspaceWiring) converge(cmd *cobra.Command) error {
	b, err := w.blueprint()
	if err != nil {
		return err
	}
	home, err := w.boxHome()
	if err != nil {
		return fmt.Errorf("locate the smith user's home on this box: %w", err)
	}
	stdout := cmd.OutOrStdout()
	env := workspace.Env{Command: w.command, StateRoot: w.root, Owner: workspace.SmithUser{}}
	result, err := workspace.Converge(cmd.Context(), env, workspace.Plan(b, home), stdout)
	if err != nil {
		return fmt.Errorf("converge this box's workspace: %w", err)
	}
	if _, err := fmt.Fprint(stdout, result.Report()); err != nil {
		return fmt.Errorf("write workspace report: %w", err)
	}
	if result.Failed() {
		return &exitError{code: 1}
	}
	return nil
}
