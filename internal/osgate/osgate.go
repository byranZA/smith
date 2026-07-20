// Package osgate is the OS support gate: it decides whether a box's operating
// system is one smith supports.
//
// The decision here is the floor alone, never an enumeration of releases: a box
// is supported when it is an Ubuntu LTS release at or above the 24.04 floor,
// with no upper bound, so future LTS releases pass without a code change. The
// capabilities smith actually needs (apt, systemd, ...) are not gated here — a
// missing capability fails loudly at bootstrap.sh's early setup guard, which
// names the capability, rather than pre-emptively at this floor gate.
package osgate

import (
	"strconv"
	"strings"
)

// floorMajor and floorMinor are the lowest supported Ubuntu LTS VERSION_ID.
const (
	floorMajor = 24
	floorMinor = 4
)

// Release holds the identity fields smith reads from /etc/os-release.
type Release struct {
	// ID is the os-release ID field, e.g. "ubuntu".
	ID string
	// Version is the os-release VERSION field, e.g. "24.04.1 LTS (Noble Numbat)".
	Version string
	// VersionID is the os-release VERSION_ID field, e.g. "24.04".
	VersionID string
}

// Decision is the outcome of the OS support gate. Reason is set only when the
// box is not supported and is a human-readable explanation.
type Decision struct {
	Supported bool
	Reason    string
}

// Parse reads /etc/os-release content into a Release. Values may be
// double-quoted; unrecognised keys are ignored.
func Parse(content string) Release {
	var r Release
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "ID":
			r.ID = value
		case "VERSION":
			r.Version = value
		case "VERSION_ID":
			r.VersionID = value
		}
	}
	return r
}

// Evaluate applies the floor gate to a Release: supported iff ID is "ubuntu",
// VERSION carries an "LTS" token, and VERSION_ID is at or above the 24.04
// floor. There is no upper bound, so an unseen future LTS passes on this rule
// alone.
func Evaluate(r Release) Decision {
	if !strings.EqualFold(r.ID, "ubuntu") {
		return Decision{Reason: "not an Ubuntu box"}
	}
	if !hasLTSToken(r.Version) {
		return Decision{Reason: "not an LTS release"}
	}
	if !atOrAboveFloor(r.VersionID) {
		return Decision{Reason: "below the 24.04 LTS floor"}
	}
	return Decision{Supported: true}
}

// hasLTSToken reports whether version carries "LTS" as a whitespace-delimited
// token (case-insensitive), so "24.04.1 LTS (Noble)" matches but a stray
// substring would not.
func hasLTSToken(version string) bool {
	for _, field := range strings.Fields(version) {
		if strings.EqualFold(field, "LTS") {
			return true
		}
	}
	return false
}

// atOrAboveFloor reports whether a "major.minor" VERSION_ID is at or above the
// supported floor. An unparseable VERSION_ID is treated as below the floor.
func atOrAboveFloor(versionID string) bool {
	major, minor, ok := majorMinor(versionID)
	if !ok {
		return false
	}
	if major != floorMajor {
		return major > floorMajor
	}
	return minor >= floorMinor
}

// majorMinor splits a "major.minor" version into its numeric parts. A missing
// minor is treated as zero; a non-numeric major reports ok=false.
func majorMinor(versionID string) (major, minor int, ok bool) {
	majorStr, minorStr, _ := strings.Cut(versionID, ".")
	major, err := strconv.Atoi(strings.TrimSpace(majorStr))
	if err != nil {
		return 0, 0, false
	}
	if minorStr != "" {
		minor, err = strconv.Atoi(strings.TrimSpace(minorStr))
		if err != nil {
			return 0, 0, false
		}
	}
	return major, minor, true
}
