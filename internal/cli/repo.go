package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/repofile"
)

// workdirResolver names the directory smith was run from. It is passed in so a
// test drives the real command from a directory of its choosing.
type workdirResolver func() (string, error)

// newRepoCmd builds `smith repo`, the verbs that work with the repo file of
// the git repo smith is run from. None is relayed to a box.
func newRepoCmd(resolve homeResolver, git repofile.Runner, workdir workdirResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Work with the repo file, the loop settings a repo commits",
	}
	cmd.AddCommand(newRepoInitCmd(resolve, git, workdir))
	return cmd
}

// newRepoInitCmd builds `smith repo init`, which writes a commented starter
// repo file at the root of the current git repo unless one is already there:
// one preset, filled in from the flags, with the work role pointed at it.
func newRepoInitCmd(resolve homeResolver, git repofile.Runner, workdir workdirResolver) *cobra.Command {
	var values repofile.Preset
	cmd := &cobra.Command{
		Use:         "init",
		Annotations: verbTraits(false, false, false),
		Short:       "Scaffold the repo file with a commented starter",
		Long: `Write a commented starter repo file, .smith/repo.yaml, at the root of the git
repo smith is run from, unless one is already there. It holds one preset,
filled in from the flags, with the work role pointed at it.

` + exits(0, 1),
		Example: `  smith repo init
  smith repo init --agent codex --effort high`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			dir, err := workdir()
			if err != nil {
				return err
			}
			repo, err := repofile.Locate(cmd.Context(), git, dir, home)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			outcomes, err := repofile.Scaffold(repo, values)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), repoOutcomeLines(outcomes)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&values.Agent, "agent", "", "the agent of the starter's preset, which is named for it (default claude)")
	cmd.Flags().StringVar(&values.Model, "model", "", "the model of the starter's preset")
	cmd.Flags().StringVar(&values.Effort, "effort", "", "the effort of the starter's preset (low, medium, high)")
	return cmd
}

// repoOutcomeLines reports each scaffolded file as created or left alone.
func repoOutcomeLines(outcomes []repofile.Outcome) string {
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

// currentDir locates the directory smith was run from.
func currentDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate the current directory: %w", err)
	}
	return dir, nil
}
