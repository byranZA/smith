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
	var asJSON *bool
	cmd := &cobra.Command{
		Use:         "check [<name-or-path>]",
		Annotations: verbTraits(false, false, false),
		Short:       "Validate a blueprint and print what smith would use",
		Long: `Validate a blueprint, named in the config home or given as a path, and print
the resolved configuration smith would use: --access over the blueprint over
preferences. With no blueprint named it checks preferences alone. It reaches
no box. A valid blueprint exits 0 whether or not its references resolve.

` + exits(0, 1),
		Example: `  smith blueprint check
  smith blueprint check personal
  smith blueprint check ./api.yaml --access tailscale`,
		Args: cobra.MaximumNArgs(1),
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
			result, err := checkBlueprint(home, args, accessMode)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if *asJSON {
				return printCheck(cmd, newCheckDocument(result))
			}
			return writeCheck(cmd, result)
		},
	}
	cmd.Flags().StringVar(&accessMode, "access", "", "override how the box is reached: public or tailscale")
	asJSON = addJSONFlag(cmd)
	return cmd
}

// writeValid reports a valid blueprint and whether `machine setup` would
// resolve everything it declares, listing what it would not by scope and reason
// but never by value.
func writeValid(cmd *cobra.Command, r checkResult) error {
	report := fmt.Sprintf("blueprint %q is valid (%s)", r.Name, r.Doc.Path)
	references, literals := r.References, r.Literals
	if references.Count()+literals.Count() > 0 {
		report += ", but `smith machine setup` from here would refuse it:\n" +
			section(references, "reference(s) will not resolve on this machine") +
			section(literals, "literal value(s) declare nothing")
	} else {
		report += "\nevery reference it declares resolves on this machine\n"
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// checkReferences resolves what the blueprint declares on this machine and
// returns what would not, split into references and empty literals. It is the
// one answer both the prose and the JSON renderers present.
func checkReferences(doc config.Document, resolution staging.Resolution) (references, literals *staging.UnresolvedError, err error) {
	var unresolved *staging.UnresolvedError
	_, err = resolveBlueprint(doc, resolution)
	switch {
	case errors.As(err, &unresolved):
		references, literals = splitLiterals(unresolved)
		return references, literals, nil
	case err != nil:
		return nil, nil, err
	}
	empty := &staging.UnresolvedError{}
	return empty, empty, nil
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

// checkResult is everything one check of preferences and an optional blueprint
// found. Both renderers present it; neither loads or validates anything itself.
type checkResult struct {
	Prefs      config.Preferences
	Name       string
	Doc        config.Document
	HasDoc     bool
	Resolved   *config.Resolved
	References *staging.UnresolvedError
	Literals   *staging.UnresolvedError
	// Findings is the validation failure that stopped the check, if any.
	Findings error
}

// checkBlueprint loads preferences and the named blueprint, resolves them and
// checks the result for conflicts and unresolved references. A file that
// fails validation is a Findings on the result; a file that cannot be read at
// all is the returned error.
func checkBlueprint(home config.Home, args []string, accessMode string) (checkResult, error) {
	var r checkResult
	prefs, err := config.LoadPreferences(home)
	r.Prefs = prefs
	if err != nil {
		if _, ok := problemsOf(err); ok {
			// A file that fails validation was there to be read.
			r.Prefs.Found = true
			r.Findings = err
			return r, nil
		}
		return r, err //nolint:wrapcheck // reportInvalid prints the cause verbatim.
	}
	overrides := config.Overrides{Access: accessMode}
	if len(args) == 0 {
		resolved := config.Resolve(overrides, nil, &prefs.Declared)
		r.Resolved = &resolved
		return r, nil
	}
	r.Name = args[0]
	r.HasDoc = true
	doc, err := config.LoadDocument(home, r.Name)
	r.Doc = doc
	if err != nil {
		if _, ok := problemsOf(err); ok {
			r.Findings = err
			return r, nil
		}
		return r, err //nolint:wrapcheck // reportInvalid prints the cause verbatim.
	}
	// The resolved picture is what setup would act on, and a
	// preference-declared field can disagree with a blueprint one, so
	// it is checked before anything is reported as valid.
	resolved := config.Resolve(overrides, &doc.Blueprint, &prefs.Declared)
	if err := resolved.Conflicts(); err != nil {
		r.Findings = err
		return r, nil
	}
	r.References, r.Literals, err = checkReferences(doc, resolutionToStage(resolved))
	if err != nil {
		return r, err
	}
	r.Resolved = &resolved
	return r, nil
}

// writeCheck prints r as prose. Preferences are part of what smith would use on
// every run, so they are validated whether or not a blueprint was named, but
// only reported when none was.
func writeCheck(cmd *cobra.Command, r checkResult) error {
	if r.Findings != nil {
		return reportInvalid(cmd, r.Findings)
	}
	if !r.HasDoc {
		line := fmt.Sprintf("preferences file %s is valid\n", r.Prefs.Path)
		if !r.Prefs.Found {
			line = fmt.Sprintf("no preferences file at %s; using smith's built-in defaults\n", r.Prefs.Path)
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), line); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	} else if err := writeValid(cmd, r); err != nil {
		return err
	}
	return writeResolved(cmd, *r.Resolved)
}

// reportInvalid writes a refusal to stderr and carries the invalid-blueprint
// exit code out, leaving stdout empty so the two streams compose separately.
func reportInvalid(cmd *cobra.Command, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: 1}
}
