package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/selfupdate"
)

// updateWiring is what `smith update` reaches beyond its own process: the
// updater with its platform, GitHub and binary, the process switch, the
// operator's terminal and the boxes.
type updateWiring struct {
	// updater runs the local half of the update.
	updater selfupdate.Updater
	// exec replaces this process with the binary at path, run with args.
	exec func(path string, args []string) error
	// term is the operator's terminal the box question is asked on.
	term terminal
	// home locates the config home whose inventory lists the boxes.
	home homeResolver
	// converge converges one named box, as `machine upgrade` does.
	converge namedBoxConverger
}

// systemUpdate is `smith update` as the operator's machine runs it: this
// platform, GitHub over HTTPS, the running binary, the operator's terminal and
// the convergence `machine upgrade` runs.
func systemUpdate() updateWiring {
	return updateWiring{
		updater: selfupdate.Updater{
			Version:    resolveVersion(),
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			MarkerPath: marker.Path,
			Get:        selfupdate.HTTP{Client: &http.Client{Timeout: 5 * time.Minute}},
			Executable: os.Executable,
			Continues:  selfupdate.Probe,
		},
		exec:     switchProcess,
		term:     secret.NewStdTerminal(),
		home:     userConfigHome,
		converge: convergeNamedBox(userConfigHome, connection.System()),
	}
}

// newUpdateCmd builds `smith update`. It replaces the local binary with a
// release (the latest, or the one --version names) and switches into it, which
// offers to converge every box to its version, an offer --boxes or --yes
// accepts.
func newUpdateCmd(w updateWiring) *cobra.Command {
	var version string
	var boxes, continuing bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update local smith to the latest release, then offer to upgrade every box",
		Long: help(`Replace this smith binary with the latest release, or the one --version
names, then offer to converge every box in the inventory to it, as
"smith machine upgrade" does; --boxes upgrades them without asking. An update
refused before anything was fetched or replaced exits 2, and a box that fails
to upgrade exits 1 after the rest have been tried.`, "With nobody at the terminal no box is upgraded: each box's upgrade command is printed instead, unless --boxes or --yes upgrades them all.") + "\n\n" + exits(0, 1, 2),
		Example: `  smith update
  smith update --version 0.4.0 --boxes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			boxes = boxes || assumeYes(cmd)
			if continuing {
				return w.upgradeBoxes(cmd, boxes)
			}
			outcome, err := w.updater.Run(cmd.Context(), version)
			var refused *selfupdate.RefusedError
			if errors.As(err, &refused) {
				return refuseUpdate(cmd.ErrOrStderr(), refused)
			}
			if err != nil {
				return fmt.Errorf("update local smith: %w", err)
			}
			if err := writeOutcome(cmd.OutOrStdout(), outcome); err != nil {
				return err
			}
			switch outcome.Next {
			case selfupdate.StepBoxesHere:
				return w.upgradeBoxes(cmd, boxes)
			case selfupdate.StepHandOff:
				return w.handOffBoxes(cmd, outcome.Target)
			default:
				return w.switchInto(outcome.Path, boxes)
			}
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "the released smith version to install; omitted, the latest release")
	cmd.Flags().BoolVar(&boxes, "boxes", false, "upgrade every box in the inventory without asking")
	cmd.Flags().BoolVar(&continuing, selfupdate.ContinueFlag, false, "run the box step alone, after a switch to the new binary")
	if err := cmd.Flags().MarkHidden(selfupdate.ContinueFlag); err != nil {
		panic(fmt.Sprintf("hide --%s: %v", selfupdate.ContinueFlag, err))
	}
	return cmd
}

// writeOutcome reports what the local half of the update did.
func writeOutcome(out io.Writer, o selfupdate.Outcome) error {
	line := fmt.Sprintf("local smith %s → %s (%s)\n", o.From, o.Target, o.Path)
	if o.Path == "" {
		line = fmt.Sprintf("local smith is already %s\n", o.Target)
	}
	if _, err := fmt.Fprint(out, line); err != nil {
		return fmt.Errorf("write update report: %w", err)
	}
	return nil
}

// switchInto replaces this process with the new binary at path, continuing
// with the box step alone.
func (w updateWiring) switchInto(path string, boxes bool) error {
	args := []string{path, "update", "--" + selfupdate.ContinueFlag}
	if boxes {
		args = append(args, "--boxes")
	}
	if err := w.exec(path, args); err != nil {
		return fmt.Errorf("start the new smith at %s: %w", path, err)
	}
	return nil
}

// upgradeBoxes is the box step: it converges the inventory's boxes to the
// version this smith runs, reports each one, and fails when any box did.
func (w updateWiring) upgradeBoxes(cmd *cobra.Command, assumeYes bool) error {
	names, err := w.boxNames(cmd)
	if err != nil || len(names) == 0 {
		return err
	}
	out := cmd.OutOrStdout()
	step := selfupdate.Boxes{
		Version: w.updater.Version,
		Ask:     w.ask,
		Converge: func(ctx context.Context, box, version string) (onbox.Result, error) {
			converged, err := w.converge(ctx, box, version)
			return converged.Result, err
		},
		Starting: func(box string) error {
			if _, err := fmt.Fprintf(out, "upgrading box %s to %s…\n", box, w.updater.Version); err != nil {
				return fmt.Errorf("write box progress: %w", err)
			}
			return nil
		},
	}
	fleet, err := step.Upgrade(cmd.Context(), names, assumeYes)
	if err != nil {
		return fmt.Errorf("upgrade the boxes: %w", err)
	}
	if err := writeFleet(out, fleet); err != nil {
		return err
	}
	if failed := fleet.Failed(); failed > 0 {
		if _, err := fmt.Fprintf(out, "local smith %s: the local update succeeded; %d of %d boxes failed\n", w.updater.Version, failed, len(fleet)); err != nil {
			return fmt.Errorf("write box summary: %w", err)
		}
		return &exitError{code: 1}
	}
	return nil
}

// handOffBoxes ends an update to a release too old to run the box step,
// leaving each box's upgrade command for the operator.
func (w updateWiring) handOffBoxes(cmd *cobra.Command, target string) error {
	names, err := w.boxNames(cmd)
	if err != nil || len(names) == 0 {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "smith %s predates smith update, so it cannot upgrade the boxes itself.\n", target); err != nil {
		return fmt.Errorf("write box report: %w", err)
	}
	return writeFleet(cmd.OutOrStdout(), selfupdate.Skipped(names))
}

// boxNames returns the inventory's boxes in name order, saying so when there
// are none.
func (w updateWiring) boxNames(cmd *cobra.Command) ([]string, error) {
	home, err := w.home()
	if err != nil {
		return nil, err
	}
	inv, _, err := inventory.Read(home.InventoryPath())
	if err != nil {
		return nil, fmt.Errorf("list the boxes to upgrade: %w", err)
	}
	if len(inv.Boxes) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "no boxes in the inventory to upgrade"); err != nil {
			return nil, fmt.Errorf("write box report: %w", err)
		}
	}
	return slices.Sorted(maps.Keys(inv.Boxes)), nil
}

// ask puts one question covering every box to the operator's terminal and
// reports whether they said yes. With no one there nothing is asked.
func (w updateWiring) ask(names []string) (bool, error) {
	if w.term == nil || !w.term.Attended() {
		return false, nil
	}
	question := fmt.Sprintf("Upgrade %d boxes (%s) to %s? [y/N] ", len(names), strings.Join(names, ", "), w.updater.Version)
	answer, err := w.term.ReadLine(question)
	if err != nil {
		return false, fmt.Errorf("ask whether to upgrade the boxes: %w", err)
	}
	return accepted(answer), nil
}

// writeFleet reports the box step: the upgrade command for each skipped box,
// then one summary line per box.
func writeFleet(out io.Writer, fleet selfupdate.Fleet) error {
	var b strings.Builder
	var skipped []string
	for _, o := range fleet {
		if o.Status == selfupdate.StatusSkipped {
			skipped = append(skipped, o.Box)
		}
	}
	if len(skipped) > 0 {
		b.WriteString("upgrade each box when you are ready:\n")
		for _, name := range skipped {
			fmt.Fprintf(&b, "  %s %s\n", upgradeCommand, name)
		}
	}
	for _, o := range fleet {
		b.WriteString(boxLine(o))
	}
	if _, err := fmt.Fprint(out, b.String()); err != nil {
		return fmt.Errorf("write box summary: %w", err)
	}
	return nil
}

// boxLine renders the summary line for one box.
func boxLine(o selfupdate.BoxOutcome) string {
	switch o.Status {
	case selfupdate.StatusFailed:
		return fmt.Sprintf("box %s: failed: %v\n", o.Box, o.Err)
	case selfupdate.StatusSkipped:
		return fmt.Sprintf("box %s: skipped\n", o.Box)
	case selfupdate.StatusCurrent:
		return fmt.Sprintf("box %s: already current at %s\n", o.Box, o.Result.Version)
	case selfupdate.StatusUpgraded:
		if o.Result.Previous == "" {
			return fmt.Sprintf("box %s: upgraded to %s\n", o.Box, o.Result.Version)
		}
		return fmt.Sprintf("box %s: upgraded %s → %s\n", o.Box, o.Result.Previous, o.Result.Version)
	default:
		return fmt.Sprintf("box %s: unknown outcome\n", o.Box)
	}
}

// refuseUpdate reports an update smith declined before anything was fetched or
// replaced, and exits as a gate rejection.
func refuseUpdate(stderr io.Writer, cause error) error {
	if _, err := fmt.Fprintf(stderr, "update refused: %v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: bootstrap.OutcomeRejected.ExitCode()}
}
