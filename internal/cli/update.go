package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/release"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/selfupdate"
)

// updateWiring is what `smith update` reaches beyond its own process: the
// platform it runs on, GitHub, the installed binary, the process switch, the
// operator's terminal and the boxes.
type updateWiring struct {
	// version is the version this smith runs.
	version string
	// goos and goarch name the platform whose release asset replaces this
	// binary.
	goos, goarch string
	// markerPath is where a box's bootstrap marker lives; its presence means
	// this smith is on a box.
	markerPath string
	// get reaches GitHub.
	get selfupdate.Getter
	// executable returns the path of the running smith binary.
	executable func() (string, error)
	// exec replaces this process with the binary at path, run with args.
	exec func(path string, args []string) error
	// continues reports whether the smith binary at path can run the box step
	// it would be switched into.
	continues func(ctx context.Context, path string) bool
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
		version:    resolveVersion(),
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
		markerPath: marker.Path,
		get:        selfupdate.HTTP{Client: &http.Client{Timeout: 5 * time.Minute}},
		executable: os.Executable,
		exec:       switchProcess,
		continues:  acceptsContinuation,
		term:       secret.NewStdTerminal(),
		home:       userConfigHome,
		converge:   convergeNamedBox(userConfigHome, connection.System()),
	}
}

// continueBoxesFlag is the hidden flag a freshly installed smith is started
// with after `smith update` replaced the old one: it runs the box step alone,
// on the new code, and never repeats the local update.
const continueBoxesFlag = "continue-boxes"

// newUpdateCmd builds `smith update`. It replaces the local binary with a
// release (the latest, or the one --version names), replaces its own process
// with the new binary, and that binary offers to converge every box in the
// inventory to its version.
func newUpdateCmd(w updateWiring) *cobra.Command {
	var version string
	var boxes, continuing bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update local smith to the latest release, then offer to upgrade every box",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if continuing {
				return w.upgradeBoxes(cmd, boxes)
			}
			if err := w.refusal(); err != nil {
				return refuseUpdate(cmd.ErrOrStderr(), err)
			}
			target, err := w.target(cmd, version)
			if err != nil {
				return err
			}
			if release.Installable(w.version) == target {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "local smith is already %s\n", target); err != nil {
					return fmt.Errorf("write update report: %w", err)
				}
				return w.upgradeBoxes(cmd, boxes)
			}
			replaced, err := w.install(cmd, target)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "local smith %s → %s (%s)\n", w.version, target, replaced); err != nil {
				return fmt.Errorf("write update report: %w", err)
			}
			if !w.continues(cmd.Context(), replaced) {
				return w.handOffBoxes(cmd, target)
			}
			args := []string{replaced, "update", "--" + continueBoxesFlag}
			if boxes {
				args = append(args, "--boxes")
			}
			if err := w.exec(replaced, args); err != nil {
				return fmt.Errorf("start the new smith at %s: %w", replaced, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "the released smith version to install; omitted, the latest release")
	cmd.Flags().BoolVar(&boxes, "boxes", false, "upgrade every box in the inventory without asking")
	cmd.Flags().BoolVar(&continuing, continueBoxesFlag, false, "run the box step alone, after a switch to the new binary")
	if err := cmd.Flags().MarkHidden(continueBoxesFlag); err != nil {
		panic(fmt.Sprintf("hide --%s: %v", continueBoxesFlag, err))
	}
	return cmd
}

// target returns the release version to update to, without the tag's "v": the
// one the operator named, spelt as --smith-version accepts it, else the latest
// published release. A named version no release carries is refused.
func (w updateWiring) target(cmd *cobra.Command, named string) (string, error) {
	if named == "" {
		latest, err := selfupdate.Latest(cmd.Context(), w.get)
		if err != nil {
			return "", fmt.Errorf("pick the version to update to: %w", err)
		}
		return latest, nil
	}
	version := release.Installable(named)
	if version == "" {
		return "", refuseUpdate(cmd.ErrOrStderr(), fmt.Errorf("%q names no published smith release", named))
	}
	return version, nil
}

// install replaces the running binary with the target release and returns the
// path of the file it replaced.
func (w updateWiring) install(cmd *cobra.Command, target string) (string, error) {
	asset, err := release.ForPlatform(target, w.goos, w.goarch)
	if err != nil {
		return "", fmt.Errorf("update to smith %s: %w", target, err)
	}
	exe, err := w.executable()
	if err != nil {
		return "", fmt.Errorf("locate the running smith: %w", err)
	}
	replaced, err := selfupdate.Install(cmd.Context(), w.get, asset, exe)
	if err != nil {
		return "", fmt.Errorf("update to smith %s: %w", target, err)
	}
	return replaced, nil
}

// upgradeBoxes is the box step: it converges the boxes in the inventory to the
// version this smith runs, when the operator agrees or --boxes already did.
func (w updateWiring) upgradeBoxes(cmd *cobra.Command, assumeYes bool) error {
	names, err := w.boxNames(cmd)
	if err != nil || len(names) == 0 {
		return err
	}
	agreed := assumeYes
	if !agreed {
		if agreed, err = w.agree(names); err != nil {
			return err
		}
	}
	if !agreed {
		return skipBoxes(cmd, names)
	}
	var summary strings.Builder
	failed := 0
	for _, name := range names {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "upgrading box %s to %s…\n", name, w.version); err != nil {
			return fmt.Errorf("write box progress: %w", err)
		}
		line, ok := w.upgradeBox(cmd, name)
		summary.WriteString(line)
		if !ok {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(&summary, "local smith %s: the local update succeeded; %d of %d boxes failed\n", w.version, failed, len(names))
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), summary.String()); err != nil {
		return fmt.Errorf("write box summary: %w", err)
	}
	if failed > 0 {
		return &exitError{code: 1}
	}
	return nil
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

// handOffBoxes ends an update to a release too old to run the box step: rather
// than switching into a binary that would refuse it, it leaves each box's
// upgrade command for the operator.
func (w updateWiring) handOffBoxes(cmd *cobra.Command, target string) error {
	names, err := w.boxNames(cmd)
	if err != nil || len(names) == 0 {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "smith %s predates smith update, so it cannot upgrade the boxes itself.\n", target); err != nil {
		return fmt.Errorf("write box report: %w", err)
	}
	return skipBoxes(cmd, names)
}

// agree asks the operator one question covering every box and reports whether
// they said yes. With no one at the terminal nothing is asked and the answer is
// no.
func (w updateWiring) agree(names []string) (bool, error) {
	if w.term == nil || !w.term.Attended() {
		return false, nil
	}
	question := fmt.Sprintf("Upgrade %d boxes (%s) to %s? [y/N] ", len(names), strings.Join(names, ", "), w.version)
	answer, err := w.term.ReadLine(question)
	if err != nil {
		return false, fmt.Errorf("ask whether to upgrade the boxes: %w", err)
	}
	return accepted(answer), nil
}

// skipBoxes reports boxes the operator did not agree to upgrade: the command
// that upgrades each one later, then a summary marking every box skipped.
func skipBoxes(cmd *cobra.Command, names []string) error {
	var b strings.Builder
	b.WriteString("upgrade each box when you are ready:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "  %s %s\n", upgradeCommand, name)
	}
	for _, name := range names {
		fmt.Fprintf(&b, "box %s: skipped\n", name)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), b.String()); err != nil {
		return fmt.Errorf("write box summary: %w", err)
	}
	return nil
}

// upgradeBox converges one box to the version this smith runs and returns the
// summary line saying what became of it, and whether it converged.
func (w updateWiring) upgradeBox(cmd *cobra.Command, name string) (string, bool) {
	converged, err := w.converge(cmd.Context(), name, w.version)
	if err != nil {
		return fmt.Sprintf("box %s: failed: %v\n", name, err), false
	}
	r := converged.Result
	switch {
	case !r.Changed:
		return fmt.Sprintf("box %s: already current at %s\n", name, r.Version), true
	case r.Previous == "":
		return fmt.Sprintf("box %s: upgraded to %s\n", name, r.Version), true
	default:
		return fmt.Sprintf("box %s: upgraded %s → %s\n", name, r.Previous, r.Version), true
	}
}

// acceptsContinuation reports whether the smith binary at path knows the
// hidden flag the box step is continued with, by asking it for that flag's
// help. A release older than `smith update` has no such command and fails.
func acceptsContinuation(ctx context.Context, path string) bool {
	probe := exec.CommandContext(ctx, path, "update", "--"+continueBoxesFlag, "--help") // #nosec G204 -- path is the smith binary update just installed.
	return probe.Run() == nil
}

// refusal returns why this smith may not update itself, or nil when it may.
// Windows cannot rename over a running binary, and a box's version is driven
// from the operator's machine rather than by the box itself.
func (w updateWiring) refusal() error {
	if w.goos == "windows" {
		return errors.New("smith update does not run on Windows yet: install the new release by hand, as docs/install.md describes")
	}
	_, err := os.Stat(w.markerPath)
	if err == nil {
		return fmt.Errorf(`this is a smith box (%s exists), and a box's smith version is driven from the operator's machine.
Run smith update there, or from there:
  smith machine upgrade <box>`, w.markerPath)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check for the box marker %s: %w", w.markerPath, err)
	}
	return nil
}

// refuseUpdate reports an update smith declined before anything was fetched or
// replaced, and exits as a gate rejection.
func refuseUpdate(stderr io.Writer, cause error) error {
	if _, err := fmt.Fprintf(stderr, "update refused: %v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: bootstrap.OutcomeRejected.ExitCode()}
}
