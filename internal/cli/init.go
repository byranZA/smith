package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/starter"
)

// newInitCmd builds `smith init`, which scaffolds the operator's config home
// with starters: it creates the home and writes a starter preferences file and
// a starter blueprint wherever no file is already there. It takes no arguments
// and asks no questions, is never relayed to a box, and reports every file as
// created or left alone before pointing at the next two commands. Exit 0
// unless a write fails, which is reported naming the path.
func newInitCmd(resolve homeResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold the config home with starter preferences and a starter blueprint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			outcomes, scaffoldErr := starter.Scaffold(home, blueprint.Git{})
			// What was settled before a failure is still reported, so the
			// operator knows which files are theirs to look at.
			if _, err := fmt.Fprint(cmd.OutOrStdout(), outcomeLines(outcomes)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if scaffoldErr != nil {
				return reportInvalid(cmd, scaffoldErr)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), nextSteps); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
}

// nextSteps is how init ends: the two commands that take a scaffolded config
// home to a built box.
var nextSteps = fmt.Sprintf("\nnext:\n  smith blueprint check %s\n  smith machine setup <login>@<host> --blueprint %s\n", starter.Blueprint, starter.Blueprint)

// outcomeLines renders one line per starter, saying whether it was created or
// an existing file was left alone.
func outcomeLines(outcomes []starter.Outcome) string {
	var b strings.Builder
	for _, o := range outcomes {
		verb := "left alone"
		if o.Created {
			verb = "created"
		}
		fmt.Fprintf(&b, "%s %s\n", verb, o.Path)
	}
	return b.String()
}
