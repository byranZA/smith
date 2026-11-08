package cli

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// jsonFlagUsage is the help every read verb's --json flag carries, pointing at the topic that documents the shapes.
const jsonFlagUsage = "print the answer as one JSON document on stdout; see `smith help json`"

// helpJSON is the text of the `smith help json` topic.
//
//go:embed help_json.md
var helpJSON string

// addJSONFlag registers the --json flag on a read verb and returns the variable it sets.
func addJSONFlag(cmd *cobra.Command) *bool {
	return cmd.Flags().Bool("json", false, jsonFlagUsage)
}

// writeDocument prints doc as the one JSON object a read verb puts on stdout, newline-terminated.
func writeDocument(w io.Writer, doc any) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode json document: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write json document: %w", err)
	}
	return nil
}

// newJSONTopic builds the `smith help json` help topic. It has no Run, so cobra lists it under "Additional help topics".
func newJSONTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "json",
		Short: "The JSON documents the read verbs print under --json",
		Long:  helpJSON,
	}
}
