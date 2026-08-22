package bootstrap

import (
	"strings"
	"testing"
)

// TestAptLockArgsParsedFromScript proves AptLockArgs is the lock-wait the
// embedded script's own apt-get calls carry, expanded and ready to hand to
// apt-get — the single definition the Go side reads instead of restating the
// budget.
func TestAptLockArgsParsedFromScript(t *testing.T) {
	want := "-o DPkg::Lock::Timeout=180"
	if got := strings.Join(AptLockArgs(), " "); got != want {
		t.Errorf("AptLockArgs() = %q, want %q", got, want)
	}
}

// TestAptLockArgsIsACopy proves a caller that mutates the arguments it was
// handed cannot change what the next caller waits with.
func TestAptLockArgsIsACopy(t *testing.T) {
	args := AptLockArgs()
	args[0] = "mutated"
	if got := AptLockArgs()[0]; got != "-o" {
		t.Errorf("AptLockArgs()[0] = %q after a caller mutated its copy, want %q", got, "-o")
	}
}

// TestParseAptLockArgsRejectsMalformedScripts proves parseAptLockArgs fails
// loudly rather than silently handing apt-get an empty or unexpanded wait: a
// missing options array, a missing budget, and an array holding nothing.
func TestParseAptLockArgsRejectsMalformedScripts(t *testing.T) {
	cases := map[string]string{
		"missing options": "APT_LOCK_TIMEOUT=180\n",
		"missing timeout": "APT_LOCK_OPTS=(-o \"DPkg::Lock::Timeout=${APT_LOCK_TIMEOUT}\")\n",
		"empty options":   "APT_LOCK_TIMEOUT=180\nAPT_LOCK_OPTS=()\n",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAptLockArgs(script); err == nil {
				t.Errorf("parseAptLockArgs(%q) succeeded, want error", script)
			}
		})
	}
}
