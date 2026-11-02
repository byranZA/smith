package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/staging"
)

// newStageConfigCmd builds the hidden `smith machine stage-config`, which applies the tar stream on stdin under /etc/smith/. It is the on-box half of the config stage (docs/adr/0024).
func newStageConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stage-config",
		Hidden:       true,
		SilenceUsage: true,
		Short:        "Apply a staged config tree relayed on stdin",
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bundle, err := staging.Decode(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("stage-config: %w", err)
			}
			out := cmd.OutOrStdout()
			if _, err := io.WriteString(out, staging.ReportHeading); err != nil {
				return fmt.Errorf("write staging report: %w", err)
			}
			if _, err := staging.Apply(staging.Root, bundle, staging.ChownByName, out); err != nil {
				return fmt.Errorf("stage-config: %w", err)
			}
			return nil
		},
	}
}

// stageConfig relays the resolved tree to the box's `machine stage-config` in one call and streams the box's report. A nil staged tree leaves the box untouched.
func stageConfig(ctx context.Context, conn relay.InputConn, target, box, version string, staged *stagedConfig, stdout, stderr io.Writer) error {
	if staged == nil {
		return nil
	}
	stream, err := staging.Encode(staged.tree)
	if err != nil {
		return fmt.Errorf("stage blueprint %s: %w", staged.path, err)
	}
	verb := relay.Verb{Target: target, Box: box, Version: version, Args: []string{"machine", "stage-config"}, Root: true}
	if err := relay.SendWithInput(ctx, conn, verb, bytes.NewReader(stream), stdout, stderr); err != nil {
		return fmt.Errorf("stage blueprint %s: %w", staged.path, err)
	}
	return nil
}
