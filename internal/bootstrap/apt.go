package bootstrap

import (
	"fmt"
	"regexp"
	"strings"
)

// The two declarations in bootstrap.sh that together are the base layer's
// first-boot apt-lock wait: the budget, and the apt-get options carrying it.
var (
	aptLockTimeoutLine = regexp.MustCompile(`(?m)^APT_LOCK_TIMEOUT=([0-9]+)`)
	aptLockOptsLine    = regexp.MustCompile(`(?m)^APT_LOCK_OPTS=\(([^)]*)\)`)
)

// aptLockTimeoutRef is how the script's options array names the budget, which
// is expanded here because Go hands apt-get its arguments directly and has no
// shell to do it.
const aptLockTimeoutRef = "${APT_LOCK_TIMEOUT}"

// aptLockArgs is the base layer's lock-wait, parsed once from the embedded
// script so a change to the budget or the option carrying it moves both the
// box-side phase and the Go-side stage together.
var aptLockArgs = mustAptLockArgsFor(Script)

// AptLockArgs are the apt-get options that wait out a busy apt lock, as
// bootstrap.sh's own apt-get calls carry them. On a freshly booted cloud image
// cloud-init and unattended-upgrades hold the apt locks for the first minute
// or two, and these are what make an install wait that out rather than abort.
//
// They come from the script's APT_LOCK_OPTS array rather than from a second
// declaration here, so the base layer's packages phase and every Go caller
// wait the same way and cannot drift apart. A fresh slice is returned, so a
// caller appending to what it was handed cannot change what the next one
// waits with.
func AptLockArgs() []string {
	return append([]string(nil), aptLockArgs...)
}

// mustAptLockArgsFor reads the lock-wait out of the embedded script and panics
// on failure. The script is compiled into the binary, so a failure here is a
// build that cannot be correct rather than a runtime condition, and a panic
// surfaces it at package initialization.
func mustAptLockArgsFor(script string) []string {
	args, err := AptLockArgsFor(script)
	if err != nil {
		panic(fmt.Sprintf("bootstrap: parse the apt lock wait from the embedded script: %v", err))
	}
	return args
}

// AptLockArgsFor is the lock-wait a bootstrap.sh source declares, as apt-get
// options: the script's APT_LOCK_OPTS array with the budget it declares
// separately substituted in and the shell's quoting removed. AptLockArgs is
// this applied to the embedded Script; taking the source as an argument is
// what lets a caller check a script it is about to ship.
//
// It errors if either declaration is absent or the options array is empty,
// rather than returning a wait that waits for nothing.
func AptLockArgsFor(script string) ([]string, error) {
	opts := aptLockOptsLine.FindStringSubmatch(script)
	if opts == nil {
		return nil, fmt.Errorf("no APT_LOCK_OPTS=(...) array found")
	}
	timeout := aptLockTimeoutLine.FindStringSubmatch(script)
	if timeout == nil {
		return nil, fmt.Errorf("no APT_LOCK_TIMEOUT found")
	}
	fields := strings.Fields(strings.ReplaceAll(opts[1], aptLockTimeoutRef, timeout[1]))
	if len(fields) == 0 {
		return nil, fmt.Errorf("APT_LOCK_OPTS array is empty")
	}
	args := make([]string, len(fields))
	for i, f := range fields {
		args[i] = strings.Trim(f, `"'`)
	}
	return args, nil
}
