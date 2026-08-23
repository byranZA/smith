//go:build windows

package workspace

import "io/fs"

// ownerUID reports no ownership on Windows, where files carry no user id.
//
// The stage this serves runs on the box, which is Linux (ADR-0008); the
// operator's Windows binary only ever drives it from the other end of a
// connection. This exists so that binary builds, and a Claim that somehow ran
// there refuses by name rather than misreporting an owner.
func ownerUID(fs.FileInfo) (int, bool) { return 0, false }
