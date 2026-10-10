package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/boxname"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/marker"
)

// newRenameCmd builds `smith machine rename <box> <new-name>`. It renames a
// registered box on both sides that record its name — the name field of the
// box's marker and its inventory entry — through boxname, and maps what came of
// it to the operator's output and exit code.
func newRenameCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <box> <new-name>",
		Short: "Rename a box on its marker and in the inventory",
		Long: `Rename a registered box. smith connects to it over its registered target,
rewrites the name its marker records and nothing else, then moves its inventory
entry to the new name. No setup phase or stage runs.

A new name that is empty, already held by a different box, or the box's current
name is refused before anything changes, as is a box smith cannot reach.

The provider-side marker is not renamed. An adapter whose marker is the box's
own provider name keeps the old name there; the name on the box's marker is the
one smith goes by.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			inv, skew, err := inventory.Read(home.InventoryPath())
			if err != nil {
				return reportInvalid(cmd, err)
			}
			target, err := inventory.RenameTarget(inv, skew, args[0], args[1])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			change := boxname.Change{From: args[0], To: args[1], Target: target}
			err = boxname.Rename(cmd.Context(), connection.New(target, exec), change, registerRename(home))
			if err != nil {
				return reportRename(cmd, change, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), registeredReport(change.To, target, change.From)); err != nil {
				return fmt.Errorf("write rename: %w", err)
			}
			return nil
		},
	}
}

// registerRename returns the registration a rename makes: setup's own, moving
// the entry off the name the box was registered under.
func registerRename(home config.Home) boxname.Register {
	return func(c boxname.Change) error {
		_, err := registerBox(home, registration{recorded: c.From, addressed: c.Target, name: c.To, target: c.Target})
		return err
	}
}

// reportRename reports a rename that did not complete and exits with the code
// it is owed: a box that never answered is a connect failure, one carrying no
// marker a gate rejection, one whose marker smith cannot read a refusal, and a
// marker renamed that the inventory did not follow is partial, reported with
// the command that reconciles the two.
func reportRename(cmd *cobra.Command, c boxname.Change, cause error) error {
	var (
		unreachable *boxname.UnreachableError
		partial     *boxname.PartialError
		code        int
		report      = fmt.Sprintf("rename refused: %v\n", cause)
	)
	switch {
	case errors.As(cause, &unreachable):
		code = bootstrap.OutcomeConnectFailed.ExitCode()
	case errors.Is(cause, boxname.ErrNoMarker):
		code = bootstrap.OutcomeRejected.ExitCode()
	case errors.Is(cause, marker.ErrMalformed):
		code = 1
	case errors.As(cause, &partial):
		code = bootstrap.OutcomePartial.ExitCode()
		report = fmt.Sprintf(`%v

Reconcile them by running the rename again once the inventory can be written:
  smith machine rename -- %s %s
`, cause, shellWord(c.From), shellWord(c.To))
	default:
		return cause
	}
	if _, err := fmt.Fprint(cmd.ErrOrStderr(), report); err != nil {
		return fmt.Errorf("write rename failure: %w", err)
	}
	return &exitError{code: code}
}

// shellWord returns s as one shell word: unchanged when every byte is one a
// shell reads literally, single-quoted otherwise.
func shellWord(s string) string {
	if s != "" && strings.Trim(s, shellSafe) == "" {
		return s
	}
	return connection.ShellArg(s)
}

// shellSafe holds the bytes a POSIX shell reads literally in an unquoted word.
const shellSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-"
