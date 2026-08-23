package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/release"
)

// resolveVersion determines the smith version string across the three build
// provenances, honoring the one-prefix rule (the version itself never carries a
// leading "v"):
//
//   - a pipeline build injected buildVersion via -ldflags; use it verbatim
//     (GoReleaser's {{ .Version }} has already stripped the tag's "v").
//   - `go install …@v0.1.0` leaves buildVersion at "dev", but ReadBuildInfo
//     reports Main.Version as "v0.1.0"; release.Installable strips the "v".
//   - any other build (local `go build`, `go install …@main`) has no released
//     tag to report, so it falls through to the "dev" literal.
func resolveVersion() string {
	if buildVersion != "dev" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := release.Installable(info.Main.Version); v != "" {
			return v
		}
	}
	return "dev"
}

// buildStamp is the optional VCS provenance ReadBuildInfo captures
// automatically: the commit and build time Go stamps into a binary built inside
// a git tree. Both are empty for a `go install …@version` build, which has no
// .git to read.
type buildStamp struct {
	commit string
	date   string
}

// readBuildStamp reads the VCS commit and build time Go records in the binary,
// returning a zero-valued stamp when no build info or VCS stamps are present.
func readBuildStamp() buildStamp {
	var s buildStamp
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return s
	}
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			s.commit = kv.Value
		case "vcs.time":
			s.date = kv.Value
		}
	}
	return s
}

// newVersionCmd builds `smith version`. It prints the resolved version and,
// when the binary was built inside a git tree, the commit and build time.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the smith version and build provenance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := fmt.Sprintf("smith %s\n", resolveVersion())
			stamp := readBuildStamp()
			if stamp.commit != "" {
				out += fmt.Sprintf("commit: %s\n", shortCommit(stamp.commit))
			}
			if stamp.date != "" {
				out += fmt.Sprintf("built:  %s\n", stamp.date)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), out); err != nil {
				return fmt.Errorf("write version: %w", err)
			}
			return nil
		},
	}
}

// shortCommit trims a full VCS revision to its first seven characters, the
// familiar abbreviated git hash, leaving anything shorter untouched.
func shortCommit(revision string) string {
	if len(revision) <= 7 {
		return revision
	}
	return revision[:7]
}
