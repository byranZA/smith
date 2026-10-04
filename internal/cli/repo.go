package cli

import (
	"fmt"
	"os"

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

// newRepoInitCmd builds `smith repo init [--agent] [--model] [--effort]`,
// which writes a commented starter repo file at the root of the git repo it is
// run from, filling in any value given as a flag, unless one is already there.
// It asks no questions, writes no prompt, and reports the file as created or
// left alone. Outside a git repo, or where the repo's .smith is the config
// home, it fails and writes nothing.
func newRepoInitCmd(resolve homeResolver, git repofile.Runner, workdir workdirResolver) *cobra.Command {
	var values repofile.File
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold the repo file with a commented starter",
		Args:  cobra.NoArgs,
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
			outcome, err := repofile.Scaffold(repo, values)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), outcomeLine(outcome)); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&values.Agent, "agent", "", "the agent to set in the starter")
	cmd.Flags().StringVar(&values.Model, "model", "", "the model to set in the starter")
	cmd.Flags().StringVar(&values.Effort, "effort", "", "the effort to set in the starter")
	return cmd
}

// outcomeLine reports the repo file as created or left alone.
func outcomeLine(o repofile.Outcome) string {
	if o.Created {
		return fmt.Sprintf("created %s\n", o.Path)
	}
	return fmt.Sprintf("left alone %s\n", o.Path)
}

// currentDir locates the directory smith was run from.
func currentDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate the current directory: %w", err)
	}
	return dir, nil
}
