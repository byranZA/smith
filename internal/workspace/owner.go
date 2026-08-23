package workspace

import (
	"fmt"
	"os"
	"syscall"
)

// Owner is the account the files smith places on a box belong to. It is the
// system boundary the stage reads and sets file ownership through, injected so
// a test drives the real converge without the root privileges a chown of
// another account's file needs.
type Owner interface {
	// Claim makes path belong to the account and reports whether it had to,
	// so a destination that already belongs to it converges as a no-op.
	Claim(path string) (bool, error)
}

// SmithUser is the account on-box smith runs as: the smith user the base layer
// provisioned and the operator connects as thereafter. It is the Owner a real
// converge run claims placed files for, and it needs no configuration —
// running as that account is what identifies it.
type SmithUser struct{}

// Claim makes path belong to the smith user and reports whether it had to.
//
// Only the owning user is reconciled, never the group: the blueprint declares
// which account a credential belongs to and leaves the group to the directory
// it sits in. A file smith itself wrote already belongs to the smith user, so
// the converged case is a stat and nothing more — the chown is for a
// destination some other account laid down, typically a root-run provisioning
// step that put a credential where the smith user could not read it.
func (SmithUser) Claim(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("read the ownership of %s: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("read the ownership of %s: the filesystem reports none", path)
	}
	uid := os.Getuid()
	if int(stat.Uid) == uid {
		return false, nil
	}
	if err := os.Chown(path, uid, -1); err != nil {
		return false, fmt.Errorf("claim %s for the smith user: %w", path, err)
	}
	return true, nil
}
