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

// TestAptLockArgsForRejectsMalformedScripts proves the lock-wait a script
// declares is refused rather than silently handed to apt-get as an empty or
// unexpanded wait: a missing options array, a missing budget, and an array
// holding nothing.
func TestAptLockArgsForRejectsMalformedScripts(t *testing.T) {
	cases := map[string]string{
		"missing options": "APT_LOCK_TIMEOUT=180\n",
		"missing timeout": "APT_LOCK_OPTS=(-o \"DPkg::Lock::Timeout=${APT_LOCK_TIMEOUT}\")\n",
		"empty options":   "APT_LOCK_TIMEOUT=180\nAPT_LOCK_OPTS=()\n",
		"empty script":    "",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := AptLockArgsFor(script); err == nil {
				t.Errorf("AptLockArgsFor(%q) succeeded, want error", script)
			}
		})
	}
}

// TestAptLockArgsForExpandsTheDeclaredBudget proves a script's own budget is
// what a caller waits with, expanded and stripped of the shell's quoting.
func TestAptLockArgsForExpandsTheDeclaredBudget(t *testing.T) {
	script := "APT_LOCK_TIMEOUT=42\nAPT_LOCK_OPTS=(-o \"DPkg::Lock::Timeout=${APT_LOCK_TIMEOUT}\")\n"
	want := "-o DPkg::Lock::Timeout=42"
	args, err := AptLockArgsFor(script)
	if err != nil {
		t.Fatalf("AptLockArgsFor(%q) = %v, want no error", script, err)
	}
	if got := strings.Join(args, " "); got != want {
		t.Errorf("AptLockArgsFor(%q) = %q, want %q", script, got, want)
	}
}
