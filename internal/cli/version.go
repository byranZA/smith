package cli

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// resolveVersion determines the smith version string across the three build
// provenances, honoring the one-prefix rule (the version itself never carries a
// leading "v"):
//
//   - a pipeline build injected buildVersion via -ldflags; use it verbatim
//     (GoReleaser's {{ .Version }} has already stripped the tag's "v").
//   - `go install …@v0.1.0` leaves buildVersion at "dev", but ReadBuildInfo
//     reports Main.Version as "v0.1.0"; strip the "v".
//   - any other build (local `go build`, `go install …@main`) has no released
//     tag to report, so it falls through to the "dev" literal.
func resolveVersion() string {
	if buildVersion != "dev" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := moduleVersion(info.Main.Version); v != "" {
			return v
		}
	}
	return "dev"
}

// pseudoVersion matches the timestamp-and-commit core Go embeds in every
// pseudo-version, e.g. the "20260728155309-f8e683ead820" in
// "v0.0.0-20260728155309-f8e683ead820": a 14-digit UTC timestamp joined to a
// 12-char commit prefix. A released tag never contains it.
var pseudoVersion = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}`)

// moduleVersion normalizes a module version reported by ReadBuildInfo into a
// smith release version, or "" when the module carries no released version.
// Only a clean tag survives: Go reports "" or "(devel)" for a bare build and a
// VCS-derived pseudo-version for a build off an untagged commit (a local
// `go build`, or `go install …@main`) — none of which is a release, so all fall
// through to the "dev" fallback. A real tag like "v0.1.0" keeps its value with
// its single leading "v" stripped per the one-prefix rule.
func moduleVersion(mainVersion string) string {
	if mainVersion == "" || mainVersion == "(devel)" || pseudoVersion.MatchString(mainVersion) {
		return ""
	}
	return strings.TrimPrefix(mainVersion, "v")
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
