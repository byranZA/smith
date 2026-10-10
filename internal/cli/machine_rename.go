package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/marker"
)

// newRenameCmd builds `smith machine rename <box> <new-name>`. It renames a
// registered box on both sides that record its name — the name field of the
// box's marker and its inventory entry — and does nothing else: no phase or
// stage runs, and the provider is never asked.
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
			from, to := args[0], args[1]
			home, err := resolve()
			if err != nil {
				return err
			}
			inv, skew, err := inventory.Read(home.InventoryPath())
			if err != nil {
				return reportInvalid(cmd, err)
			}
			target, err := renameTarget(inv, skew, from, to)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			ctx := cmd.Context()
			conn := connection.New(target, exec)
			if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
				if !errors.Is(err, connection.ErrConnect) {
					return fmt.Errorf("probe box: %w", err)
				}
				return refuseRename(cmd, bootstrap.OutcomeConnectFailed, err)
			}
			if err := renameMarker(ctx, conn, to); err != nil {
				if errors.Is(err, errNoMarker) {
					return refuseRename(cmd, bootstrap.OutcomeRejected, err)
				}
				return err
			}
			if _, err := registerBox(home, registration{recorded: from, addressed: target, name: to, target: target}); err != nil {
				return reportDisagreement(cmd, from, to, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), registeredReport(to, target, from)); err != nil {
				return fmt.Errorf("write rename: %w", err)
			}
			return nil
		},
	}
}

// renameTarget returns the target of the box registered as from, refusing every
// rename the inventory would refuse — an inventory a newer smith wrote among
// them, and one that would change nothing — before anything is touched. It decides through inventory.Rename, the path the
// registration itself takes, so the refusal is the one setup gives.
func renameTarget(inv inventory.Inventory, skew inventory.Skew, from, to string) (string, error) {
	if skew == inventory.SkewNewer {
		return "", fmt.Errorf("cannot rename %q: %w", from, inventory.ErrSkewNewer)
	}
	target, ok := inventory.Lookup(inv, from)
	if !ok {
		return "", fmt.Errorf("cannot rename %q: %w", from, &inventory.UnknownBoxError{Name: from})
	}
	if from == to {
		return "", fmt.Errorf("cannot rename %q: the box is already named %q", from, to)
	}
	if _, err := inventory.Rename(inv, from, to, target); err != nil {
		return "", fmt.Errorf("cannot rename %q: %w", from, err)
	}
	return target, nil
}

// renameMarker rewrites the name the box's marker records to name, leaving
// every other byte of the marker as it was.
func renameMarker(ctx context.Context, conn *connection.SSH, name string) error {
	var raw bytes.Buffer
	if err := conn.Run(ctx, fmt.Sprintf("cat %s 2>/dev/null || true", marker.Path), &raw, io.Discard); err != nil {
		return fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	if len(bytes.TrimSpace(raw.Bytes())) == 0 {
		return errNoMarker
	}
	renamed, err := marker.Rename(raw.Bytes(), name)
	if err != nil {
		return fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	if err := conn.RunWithInput(ctx, markerWriteCommand(), bytes.NewReader(renamed), io.Discard, io.Discard); err != nil {
		return fmt.Errorf("write the box's marker at %s: %w", marker.Path, err)
	}
	return nil
}

// errNoMarker reports a registered box that carries no marker, so there is no
// name on it to rewrite.
var errNoMarker = fmt.Errorf("the box carries no marker at %s, so smith has never set it up: "+
	"provision it first with smith machine setup", marker.Path)

// refuseRename reports a rename the box itself turned away, before anything was
// written, and exits with the code that kind of refusal is owed: a box that
// never answered is a connect failure, and one that answered with no marker is
// a gate rejection.
func refuseRename(cmd *cobra.Command, outcome bootstrap.Outcome, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "rename refused: %v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: outcome.ExitCode()}
}

// reportDisagreement reports a rename that wrote the box's marker but not the
// inventory, and exits partial. The two now disagree about the box's name, so
// the operator is told which side records which, and that running the same
// rename again reconciles them: the marker rewrite is idempotent.
func reportDisagreement(cmd *cobra.Command, from, to string, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), `the box's marker records the name %q, but the inventory still registers it as %q: %v

Reconcile them by running the rename again once the inventory can be written:
  smith machine rename %s %s
`, to, from, cause, from, to); err != nil {
		return fmt.Errorf("write rename failure: %w", err)
	}
	return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
}

// markerWriteCommand builds the remote write of the marker from stdin: into a
// temporary file beside it, given the root ownership and mode bootstrap.sh
// leaves it with, then moved into place, so a write cut short never leaves the
// box a half-written marker. Every step runs through the smith user's sudo.
func markerWriteCommand() string {
	tmp := connection.ShellArg(marker.Path + ".rename")
	dest := connection.ShellArg(marker.Path)
	return strings.Join([]string{
		fmt.Sprintf("sudo rm -f %s", tmp),
		fmt.Sprintf("sudo tee %s >/dev/null", tmp),
		fmt.Sprintf("sudo chmod 0644 %s", tmp),
		fmt.Sprintf("sudo chown root:root %s", tmp),
		fmt.Sprintf("sudo mv -f %s %s", tmp, dest),
	}, " && ")
}
