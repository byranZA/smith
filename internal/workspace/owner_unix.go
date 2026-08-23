//go:build !windows

package workspace

import (
	"io/fs"
	"syscall"
)

// ownerUID reports the user id a stat result says the file belongs to, and
// whether the filesystem reported one at all.
func ownerUID(info fs.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
