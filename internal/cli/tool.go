package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/smithtool"
)

// toolListDocument is the JSON document `tool list --json` prints.
type toolListDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Tools         []smithtool.Info `json:"tools"`
}

// newToolCmd builds `smith tool`: it asks the loop serving the socket getenv
// names in SMITH_TOOLS to run a smith tool, or to list the ones it serves.
func newToolCmd(getenv func(string) string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool <name> [args...]",
		Short: "Ask the running loop to perform a smith tool (a loop's agent only)",
		Long: `Asks the loop that is running this agent to perform the smith tool <name>,
streaming the tool's output. Only an agent a loop handed a task or the review
can use it: the loop gives it the socket in SMITH_TOOLS, and anywhere else
smith refuses. "smith tool list" names the tools this run serves.

Exits 0 when the tool succeeded, 1 when it ran and failed, or 2 when it was
refused before anything changed: no loop to ask, a dead socket, or an unknown
tool.

` + exits(0, 1, 2),
		Example:     "  smith tool stack-up",
		Annotations: verbTraits(false, false, false),
		Args:        cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			err := smithtool.Call(cmd.Context(), getenv(loop.ToolsEnv), args[0], args[1:], cmd.OutOrStdout())
			return toolExit(cmd, err)
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.AddCommand(newToolListCmd(getenv))
	return cmd
}

// newToolListCmd builds `smith tool list`.
func newToolListCmd(getenv func(string) string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the smith tools this run serves",
		Long: `Prints the smith tools the loop running this agent serves, one line each: its
name and what it does. Nothing is printed when the run serves none.

Exits 0, or 2 when there is no loop to ask.

` + exits(0, 2),
		Example:     "  smith tool list --json",
		Annotations: verbTraits(false, false, false),
		Args:        cobra.NoArgs,
	}
	asJSON := addJSONFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		infos, err := smithtool.List(cmd.Context(), getenv(loop.ToolsEnv))
		if err != nil {
			return toolExit(cmd, err)
		}
		if *asJSON {
			return writeDocument(cmd.OutOrStdout(), toolListDocument{SchemaVersion: 1, Tools: infos})
		}
		var b strings.Builder
		for _, info := range infos {
			fmt.Fprintf(&b, "%s  %s\n", info.Name, info.Summary)
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), b.String()); err != nil {
			return fmt.Errorf("write tool list: %w", err)
		}
		return nil
	}
	return cmd
}

// toolExit says on cmd's stderr why a smith tool call ended as it did and
// returns the exit it stands for: nil for success, 1 for a tool that failed
// and 2 for a call refused before anything changed.
func toolExit(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	code := 1
	var refused *smithtool.RefusedError
	if errors.As(err, &refused) {
		code = 2
	}
	if _, werr := fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", err); werr != nil {
		return fmt.Errorf("write refusal: %w", werr)
	}
	return &exitError{code: code}
}

// systemTool is the tool verb of the operator's own process, reading SMITH_TOOLS from its environment.
func systemTool() *cobra.Command { return newToolCmd(os.Getenv) }
