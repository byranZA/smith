package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/staging"
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

// newCheckCmd builds `smith blueprint check [<name-or-path>] [--access <mode>]`,
// which prints the configuration setup would use and whether its references
// resolve on this machine, without writing anything. It exits 0 for a valid
// blueprint whether or not its references resolve, and 1 for an invalid or
// missing one.
func newCheckCmd(resolve homeResolver) *cobra.Command {
	var accessMode string
	cmd := &cobra.Command{
		Use:   "check [<name-or-path>]",
		Short: "Validate a blueprint and print what smith would use",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			// The flag outranks every declared value, so an unrecognised one
			// would otherwise win the precedence chain unchallenged.
			if accessMode != "" {
				if err := blueprint.ValidateAccess(accessMode); err != nil {
					return reportInvalid(cmd, fmt.Errorf("--access %w", err))
				}
			}
			// Preferences are part of what smith would use on every run, so
			// they are validated whether or not a blueprint was named.
			prefs, err := checkPreferences(cmd, home, len(args) == 0)
			if err != nil {
				return err
			}
			overrides := config.Overrides{Access: accessMode}
			if len(args) == 0 {
				return writeResolved(cmd, config.Resolve(overrides, nil, &prefs))
			}
			name := args[0]
			doc, err := config.LoadDocument(home, name)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			// The resolved picture is what setup would act on, and a
			// preference-declared field can disagree with a blueprint one, so
			// it is checked before anything is reported as valid.
			resolved := config.Resolve(overrides, &doc.Blueprint, &prefs)
			if err := resolved.Conflicts(); err != nil {
				return reportInvalid(cmd, err)
			}
			if err := writeValid(cmd, name, doc, resolutionToStage(resolved)); err != nil {
				return err
			}
			return writeResolved(cmd, resolved)
		},
	}
	cmd.Flags().StringVar(&accessMode, "access", "", "override how the box is reached: public or tailscale")
	return cmd
}

// writeValid reports a valid blueprint and whether `machine setup` would
// resolve everything it declares, listing what it would not by scope and reason
// but never by value.
func writeValid(cmd *cobra.Command, name string, doc config.Document, resolution staging.Resolution) error {
	report := fmt.Sprintf("blueprint %q is valid (%s)", name, doc.Path)
	var unresolved *staging.UnresolvedError
	_, err := resolveBlueprint(doc, resolution)
	switch {
	case errors.As(err, &unresolved):
		references, literals := splitLiterals(unresolved)
		report += ", but `smith machine setup` from here would refuse it:\n" +
			section(references, "reference(s) will not resolve on this machine") +
			section(literals, "literal value(s) declare nothing")
	case err != nil:
		return err
	default:
		report += "\nevery reference it declares resolves on this machine\n"
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// splitLiterals separates the env values unresolved names that declare an
// empty literal: from the references that did not resolve.
func splitLiterals(unresolved *staging.UnresolvedError) (references, literals *staging.UnresolvedError) {
	references = &staging.UnresolvedError{Sources: unresolved.Sources}
	literals = &staging.UnresolvedError{}
	for _, v := range unresolved.Values {
		if blueprint.IsLiteral(v.Ref) {
			literals.Values = append(literals.Values, v)
			continue
		}
		references.Values = append(references.Values, v)
	}
	return references, literals
}

// section lists unresolved under a heading that counts it, or is empty when
// there is nothing to list.
func section(unresolved *staging.UnresolvedError, heading string) string {
	if unresolved.Count() == 0 {
		return ""
	}
	return fmt.Sprintf("%d %s:%s\n", unresolved.Count(), heading, unresolved.List())
}

// writeResolved prints the configuration smith would use, one field per line
// with the origin of its value.
func writeResolved(cmd *cobra.Command, resolved config.Resolved) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\nresolved configuration:\n%s", resolved); err != nil {
		return fmt.Errorf("write resolved configuration: %w", err)
	}
	return nil
}

// checkPreferences validates the operator's preferences and hands them back
// for the precedence chain, reporting on them when report is set — which is
// when no blueprint was named and they are the whole subject of the check.
// Under a named blueprint they are validated silently, so a valid preferences
// file does not narrate itself in front of the blueprint the operator asked
// about.
//
// Preferences and the config home holding them are both optional, so an absent
// file is reported and the command succeeds: smith falls through to its
// built-in defaults. Nothing is created.
func checkPreferences(cmd *cobra.Command, home config.Home, report bool) (blueprint.Preferences, error) {
	prefs, err := config.LoadPreferences(home)
	if err != nil {
		return blueprint.Preferences{}, reportInvalid(cmd, err)
	}
	if !report {
		return prefs.Declared, nil
	}
	line := fmt.Sprintf("preferences file %s is valid\n", prefs.Path)
	if !prefs.Found {
		line = fmt.Sprintf("no preferences file at %s; using smith's built-in defaults\n", prefs.Path)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), line); err != nil {
		return blueprint.Preferences{}, fmt.Errorf("write report: %w", err)
	}
	return prefs.Declared, nil
}

// reportInvalid writes a refusal to stderr and carries the invalid-blueprint
// exit code out, leaving stdout empty so the two streams compose separately.
func reportInvalid(cmd *cobra.Command, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: 1}
}
