package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/release"
)

// Step is what follows the local half of an update.
type Step int

const (
	// StepBoxesHere runs the box step in this process: local smith was already
	// on the target, so there is no new binary to switch into.
	StepBoxesHere Step = iota
	// StepSwitch replaces this process with the new binary, which runs the box
	// step on its own code.
	StepSwitch
	// StepHandOff leaves the boxes to the operator: the new binary predates the
	// box step and would refuse to continue it.
	StepHandOff
)

// Outcome is what the local half of an update did: the version it moved from
// and to, the binary it replaced (empty when nothing was), and what follows.
type Outcome struct {
	// From is the version local smith ran before.
	From string
	// Target is the release version local smith runs afterwards.
	Target string
	// Path is the file the new binary was renamed over, empty when local smith
	// was already on the target.
	Path string
	// Next is what follows.
	Next Step
}

// RefusedError reports an update declined before anything was fetched or
// replaced.
type RefusedError struct {
	// Reason says why, in words the operator can act on.
	Reason string
}

// Error implements error.
func (e *RefusedError) Error() string { return e.Reason }

// Updater updates the running smith binary to a published release.
type Updater struct {
	// Version is the version this smith runs.
	Version string
	// OS and Arch name the platform whose release asset replaces the binary,
	// as runtime.GOOS and runtime.GOARCH do.
	OS, Arch string
	// MarkerPath is where a box's bootstrap marker lives; its presence means
	// this smith runs on a box.
	MarkerPath string
	// Get reaches GitHub.
	Get Getter
	// Executable returns the path of the running smith binary.
	Executable func() (string, error)
	// Continues reports whether the smith binary at path can run the box step.
	Continues func(ctx context.Context, path string) (bool, error)
}

// Run updates the binary to the release named (spelt as --smith-version
// accepts it), else the latest, and says what follows. A refusal is a
// *RefusedError returned before anything is fetched.
func (u Updater) Run(ctx context.Context, named string) (Outcome, error) {
	if err := u.refusal(); err != nil {
		return Outcome{}, err
	}
	target, err := u.target(ctx, named)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{From: u.Version, Target: target}
	if release.Installable(u.Version) == target {
		outcome.Next = StepBoxesHere
		return outcome, nil
	}
	if outcome.Path, err = u.install(ctx, target); err != nil {
		return Outcome{}, err
	}
	continues, err := u.Continues(ctx, outcome.Path)
	if err != nil {
		return Outcome{}, &ProbeError{Path: outcome.Path, Target: target, Err: err}
	}
	outcome.Next = StepHandOff
	if continues {
		outcome.Next = StepSwitch
	}
	return outcome, nil
}

// refusal returns why this smith may not update itself, or nil when it may.
// Windows cannot rename over a running binary, and a box's version is driven
// from the operator's machine.
func (u Updater) refusal() error {
	if u.OS == "windows" {
		return &RefusedError{Reason: "smith update does not run on Windows yet: install the new release by hand, as docs/install.md describes"}
	}
	_, err := os.Stat(u.MarkerPath)
	if err == nil {
		return &RefusedError{Reason: fmt.Sprintf(`this is a smith box (%s exists), and a box's smith version is driven from the operator's machine.
Run smith update there, or from there:
  smith machine upgrade <box>`, u.MarkerPath)}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check for the box marker %s: %w", u.MarkerPath, err)
	}
	return nil
}

// target returns the release version to update to, without the tag's "v": the
// one named, else the latest. A named version no release carries is refused.
func (u Updater) target(ctx context.Context, named string) (string, error) {
	if named == "" {
		latest, err := Latest(ctx, u.Get)
		if err != nil {
			return "", fmt.Errorf("pick the version to update to: %w", err)
		}
		return latest, nil
	}
	version := release.Installable(named)
	if version == "" {
		return "", &RefusedError{Reason: fmt.Sprintf("%q names no published smith release", named)}
	}
	return version, nil
}

// install replaces the running binary with the target release and returns the
// path of the file it replaced.
func (u Updater) install(ctx context.Context, target string) (string, error) {
	asset, err := release.ForPlatform(target, u.OS, u.Arch)
	if err != nil {
		return "", fmt.Errorf("update to smith %s: %w", target, err)
	}
	exe, err := u.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running smith: %w", err)
	}
	replaced, err := Install(ctx, u.Get, asset, exe)
	if err != nil {
		return "", fmt.Errorf("update to smith %s: %w", target, err)
	}
	return replaced, nil
}

// Status is what the box step did to one box.
type Status int

const (
	// StatusUpgraded means the box's binary was replaced.
	StatusUpgraded Status = iota
	// StatusCurrent means the box already ran the version.
	StatusCurrent
	// StatusFailed means the box could not be reached or did not converge.
	StatusFailed
	// StatusSkipped means the box was not attempted.
	StatusSkipped
)

// BoxOutcome is what the box step did to one box: its status, what the
// convergence reported when it ran, and why it failed when it did.
type BoxOutcome struct {
	// Box is the box's inventory name.
	Box string
	// Status is what became of it.
	Status Status
	// Result is what converging the box did, when it converged.
	Result onbox.Result
	// Err is why it failed, when it did.
	Err error
}

// Fleet is the box step's outcome for every box, in the order they were taken.
type Fleet []BoxOutcome

// Failed returns how many boxes failed.
func (f Fleet) Failed() int {
	n := 0
	for _, o := range f {
		if o.Status == StatusFailed {
			n++
		}
	}
	return n
}

// Skipped is the fleet of names with none attempted.
func Skipped(names []string) Fleet {
	fleet := make(Fleet, 0, len(names))
	for _, name := range names {
		fleet = append(fleet, BoxOutcome{Box: name, Status: StatusSkipped})
	}
	return fleet
}

// Boxes is the box step: it converges boxes to the version local smith runs,
// once the operator agrees.
type Boxes struct {
	// Version is the version every box is converged to.
	Version string
	// Ask puts one question covering every named box and reports whether the
	// operator agreed; no one to ask is a no.
	Ask func(names []string) (bool, error)
	// Converge converges one box to a version, as `machine upgrade` does.
	Converge func(ctx context.Context, box, version string) (onbox.Result, error)
	// Starting, when set, is told each box as its convergence begins.
	Starting func(box string) error
}

// Upgrade converges each named box in turn, asking first unless assumeYes. A
// declined question skips every box; a box that fails is recorded and the rest
// still go ahead.
func (b Boxes) Upgrade(ctx context.Context, names []string, assumeYes bool) (Fleet, error) {
	if len(names) == 0 {
		return Fleet{}, nil
	}
	if !assumeYes {
		agreed, err := b.Ask(names)
		if err != nil {
			return nil, err
		}
		if !agreed {
			return Skipped(names), nil
		}
	}
	fleet := make(Fleet, 0, len(names))
	for _, name := range names {
		if b.Starting != nil {
			if err := b.Starting(name); err != nil {
				return nil, err
			}
		}
		fleet = append(fleet, b.upgradeBox(ctx, name))
	}
	return fleet, nil
}

// upgradeBox converges one box and classifies what became of it.
func (b Boxes) upgradeBox(ctx context.Context, name string) BoxOutcome {
	result, err := b.Converge(ctx, name, b.Version)
	if err != nil {
		return BoxOutcome{Box: name, Status: StatusFailed, Err: err}
	}
	if !result.Changed {
		return BoxOutcome{Box: name, Status: StatusCurrent, Result: result}
	}
	return BoxOutcome{Box: name, Status: StatusUpgraded, Result: result}
}
