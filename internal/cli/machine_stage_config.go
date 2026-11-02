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

// newStageConfigCmd builds the hidden `smith machine stage-config`, the on-box
// half of the config stage (docs/adr/0024). It reads the tar stream
// `machine setup` relays on stdin, checks all of it, and applies it under
// /etc/smith/, printing one line per file as it lands.
//
// It is hidden because it is the two smiths talking to each other: nobody
// types it, and it runs as root, so it touches nothing outside /etc/smith/.
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

// stageConfig runs the config-staging stage: it relays the resolved tree to the
// box's `machine stage-config` in one call, as a tar stream on stdin through
// `sudo -n`, and streams the box's report to the operator.
//
// The tree was resolved before the box was touched, so the only failures left
// are the box's own. It runs after install because the verb it relays is the
// box's own smith. Nothing to stage — a run naming no blueprint — leaves the
// box exactly as it was.
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
