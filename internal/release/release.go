// Package release names smith's published release artifacts: which versions
// have one, what a version's Linux asset is called, and where it is downloaded
// from.
//
// It is the one place the asset-naming convention lives, so the box-side
// installer and any later consumer derive the same URL rather than each
// spelling the convention out. It is pure and offline: nothing here reaches
// GitHub, opens a connection or reads a box — it renders names and classifies
// versions, and the fetching happens on the box.
//
// The naming follows the release pipeline's own defaults: archives are
// smith_<version>_linux_<arch>.tar.gz beside a checksums.txt, published under
// the tag the version came from. Tags carry the "v" prefix and nothing else
// does, which is why the version renders bare everywhere but in the tag.
package release

import (
	"fmt"
	"regexp"
	"strings"
)

// downloadBase is where a published release's assets are served from, one
// directory per tag.
const downloadBase = "https://github.com/byranZA/smith/releases/download"

// Asset is the published Linux release artifact for one version and
// architecture: the archive's name, where it is downloaded from, and where the
// checksums it is verified against are published.
type Asset struct {
	// Version is the smith version the asset carries, without the tag's "v".
	Version string
	// Arch is the release architecture the asset is built for.
	Arch string
	// Name is the archive's file name.
	Name string
	// URL is where the archive is downloaded from.
	URL string
	// ChecksumsURL is where the release's checksums.txt is downloaded from. It
	// covers every asset of the release, so the archive is verified against the
	// same file that covers the other four platforms.
	ChecksumsURL string
}

// For returns the Linux release asset for a version and a release
// architecture. The version is the bare one ("0.2.0"); the tag's "v" is added
// here, where the URL is built, and nowhere else.
func For(version, arch string) Asset {
	name := fmt.Sprintf("smith_%s_linux_%s.tar.gz", version, arch)
	tagDir := fmt.Sprintf("%s/v%s", downloadBase, version)
	return Asset{
		Version:      version,
		Arch:         arch,
		Name:         name,
		URL:          tagDir + "/" + name,
		ChecksumsURL: tagDir + "/checksums.txt",
	}
}

// UnsupportedArchError reports a machine hardware name smith publishes no
// release asset for. It carries the name the box reported so the refusal can
// name it back: the operator's box is what it is, and only the name it gave
// tells them why smith cannot serve it.
type UnsupportedArchError struct {
	// Machine is the machine hardware name the box reported, as `uname -m`
	// prints it.
	Machine string
}

// Error implements error.
func (e *UnsupportedArchError) Error() string {
	return fmt.Sprintf("no smith release asset for the machine hardware name %q", e.Machine)
}

// machineArch maps the machine hardware names `uname -m` reports on a box to
// the release architectures smith publishes assets for. Anything absent is a
// box smith cannot install onto, refused by name rather than guessed at.
var machineArch = map[string]string{
	"x86_64":  "amd64",
	"aarch64": "arm64",
}

// Arch maps a machine hardware name, as `uname -m` prints it on the box, to the
// release architecture smith publishes assets for. A name with no published
// asset is an *UnsupportedArchError naming it.
func Arch(machine string) (string, error) {
	arch, ok := machineArch[machine]
	if !ok {
		return "", &UnsupportedArchError{Machine: machine}
	}
	return arch, nil
}

// pseudoVersion matches the timestamp-and-commit core Go embeds in every
// pseudo-version, e.g. the "20260728155309-f8e683ead820" in
// "v0.0.0-20260728155309-f8e683ead820": a 14-digit UTC timestamp joined to a
// 12-char commit prefix. A released tag never contains it.
var pseudoVersion = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}`)

// Installable reports the release version a version string names, or "" when it
// names no published release. It is the single rule for "is there an asset to
// fetch for this build", applied both to the module version
// runtime/debug.ReadBuildInfo reports and to the version smith resolved for
// itself.
//
// Only a clean tag survives: Go reports "" or "(devel)" for a bare build and a
// VCS-derived pseudo-version for a build off an untagged commit (a local
// `go build`, or `go install …@main`), and smith's own fallback for such a
// build is the "dev" literal — none of which is a release. A real tag like
// "v0.1.0" keeps its value with its single leading "v" stripped per the
// one-prefix rule.
func Installable(version string) string {
	if version == "" || version == "dev" || version == "(devel)" || pseudoVersion.MatchString(version) {
		return ""
	}
	return strings.TrimPrefix(version, "v")
}
