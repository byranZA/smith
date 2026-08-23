package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/release"
)

// newUpgradeCmd builds `smith machine upgrade <name-or-target>`. It converges
// the box's smith binary to the version local smith runs, and does nothing
// else: no bootstrap phase runs, no configuration is staged, no placement is
// touched and the marker is not written. That is the whole reason it exists as
// a verb separate from `machine setup`, which reaches the same install stage as
// one step of a longer pipeline.
//
// It is local-only, and for its own reason: a box cannot replace its own binary
// at the behest of a command running from that binary. That is unrelated to why
// `machine list`/`add`/`forget` are local-only — those are local because a box
// knows of no other boxes — so the two rationales should not be read as one.
//
// The version installed is local smith's own, so the two sides match by
// construction rather than by policy. That makes the post-condition "no version
// skew" rather than "the box is newest": a box ahead of the operator is moved
// backwards, and the report says so.
//
// --smith-version installs a version the operator names instead. It is the
// escape a build with no published release needs, and it overrides a release
// build just as readily, for the operator pinning a box to an older smith.
func newUpgradeCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	var smithVersion string
	cmd := &cobra.Command{
		Use:   "upgrade <name-or-target>",
		Short: "Converge the smith binary on a box to the version local smith runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			// The version is settled before a connection is opened, so a build
			// with nothing to install refuses with the box untouched.
			version := release.Installable(chosenVersion(smithVersion, resolveVersion()))
			if version == "" {
				return refuseUpgrade(stderr, devBuildRefusal(resolveVersion(), args[0]))
			}

			inv, err := lookupInventory(resolve, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			target := inventory.Resolve(inv, args[0])

			result, err := onbox.NewInstaller(connection.New(target, exec)).Converge(cmd.Context(), version)
			if err != nil {
				return refuseUpgrade(stderr, err)
			}
			if _, err := fmt.Fprintf(stdout, "box %s: %s", args[0], result.Report()); err != nil {
				return fmt.Errorf("write upgrade report: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&smithVersion, "smith-version", "",
		"the released smith version to install; omitted, the box is converged to the version local smith runs")
	return cmd
}

// chosenVersion returns the version the operator named, else the one local
// smith runs.
func chosenVersion(chosen, local string) string {
	if chosen != "" {
		return chosen
	}
	return local
}

// refuseUpgrade reports an upgrade smith declined and exits as a gate
// rejection, the code the setup family answers a refusal with. Nothing was
// installed: every refusal here happens either before the box is reached or
// before the box replaced its binary, so the box is still a working box at the
// version it already had.
func refuseUpgrade(stderr io.Writer, cause error) error {
	if _, err := fmt.Fprintf(stderr, "upgrade refused: %v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: bootstrap.OutcomeRejected.ExitCode()}
}

// devBuildRefusal renders the refusal a build with no published release is owed.
//
// It states the whole situation rather than just the flag. An error naming only
// --smith-version walks a contributor straight into the second problem: the
// released smith that flag installs will refuse to relay a command from the dev
// build that installed it, because the box enforces the version match and a dev
// build declares a version no release has. So the refusal names the flag, the
// consequence of using it, and the two-sided fix — a released local smith.
func devBuildRefusal(version, box string) error {
	return fmt.Errorf(`this smith is a dev build (%s), so there is no release asset to install.

Name a released version to install one onto the box:
  smith machine upgrade %s --smith-version <tag>

That released smith will then refuse to relay a command from this dev build:
the box enforces that both sides run the same version, and a dev build has no
released version to match. The fix on both sides is a released local smith —
install one from https://github.com/byranZA/smith/releases and upgrade from
it`, version, box)
}
