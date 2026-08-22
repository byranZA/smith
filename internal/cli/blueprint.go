package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/config"
)

// homeResolver locates the operator's config home. It is passed into the
// blueprint command rather than called inside it, so a test drives the real
// command against a temp directory.
type homeResolver func() (config.Home, error)

// userConfigHome locates ~/.smith, the operator's config home.
func userConfigHome() (config.Home, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return config.Home{}, fmt.Errorf("locate home directory: %w", err)
	}
	return config.NewHome(filepath.Join(dir, ".smith")), nil
}

// newBlueprintCmd builds `smith blueprint` and its subcommands, reading the
// config home through resolve.
func newBlueprintCmd(resolve homeResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blueprint",
		Short: "Work with the blueprints declaring what kind of box smith builds",
	}
	cmd.AddCommand(newCheckCmd(resolve))
	return cmd
}

// newCheckCmd builds `smith blueprint check [<name-or-path>]`. With a bare
// name it reads that blueprint out of the config home; with a path it reads
// that file verbatim; with no argument it reports on the operator's
// preferences alone. The preferences are validated either way, since they are
// part of what smith would use. It reports whether what it read is valid. check
// writes nothing, touches no box, and needs no network: it answers "is this
// blueprint valid?" before any box exists. Exit 0 valid, 1 invalid or not
// found, with the report on stdout and any refusal on stderr.
func newCheckCmd(resolve homeResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "check [<name-or-path>]",
		Short: "Validate a blueprint without touching a box",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			// Preferences are part of what smith would use on every run, so
			// they are validated whether or not a blueprint was named.
			if err := checkPreferences(cmd, home, len(args) == 0); err != nil {
				return err
			}
			if len(args) == 0 {
				return nil
			}
			name := args[0]
			path, err := config.Select(home, name)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := config.Load(home, name); err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "blueprint %q is valid (%s)\n", name, path); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
}

// checkPreferences validates the operator's preferences, reporting on them
// when report is set — which is when no blueprint was named and they are the
// whole subject of the check. Under a named blueprint they are validated
// silently, so a valid preferences file does not narrate itself in front of
// the blueprint the operator asked about.
//
// Preferences and the config home holding them are both optional, so an absent
// file is reported and the command succeeds: smith falls through to its
// built-in defaults. Nothing is created.
func checkPreferences(cmd *cobra.Command, home config.Home, report bool) error {
	path, found, err := config.PreferencesFile(home)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	if !found {
		if !report {
			return nil
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "no preferences file at %s; using smith's built-in defaults\n", path); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		return nil
	}
	if _, err := config.LoadPreferences(home); err != nil {
		return reportInvalid(cmd, err)
	}
	if !report {
		return nil
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "preferences file %s is valid\n", path); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// reportInvalid writes a refusal to stderr and carries the invalid-blueprint
// exit code out, leaving stdout empty so the two streams compose separately.
func reportInvalid(cmd *cobra.Command, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: 1}
}
