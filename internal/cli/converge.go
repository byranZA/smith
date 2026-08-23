package cli

import (
	"context"
	"fmt"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/release"
)

// upgradeCommand is the command a refused convergence leaves the operator with,
// and the one whose --smith-version escape a build with no release is offered.
// Both verbs that converge a named box leave the same one: `machine upgrade` is
// the verb the operator ran, and a declined version-skew prompt has always
// pointed at it.
const upgradeCommand = "smith machine upgrade"

// convergence is what converging one box's smith binary did: the box that was
// converged, and what the install stage did to the binary there.
//
// It is returned rather than printed because each verb says a different amount
// about which box it reached: the setup pipeline is already inside a stage of a
// run against one named box, and `machine upgrade` has nothing else on the line
// to say which box moved.
type convergence struct {
	// Box is the name or address the operator typed.
	Box string
	// Result is what the install stage did to the binary on that box.
	Result onbox.Result
}

// Report renders the convergence with the box named, for a verb whose output
// says nothing else about which box it reached.
func (c convergence) Report() string {
	return fmt.Sprintf("box %s: %s", c.Box, c.Result.Report())
}

// binaryConvergence converges the smith binary on one box to a version. Install
// and upgrade are one operation on the box (see internal/onbox), and this is
// the one assembly of it above the box: the version policy, the install stage,
// and a typed result. `machine setup`'s install stage, `machine upgrade` and an
// accepted version-skew prompt all reach the box through here, so none of them
// can drift into converging a box on its own terms.
type binaryConvergence struct {
	// box is the name or address the operator typed, named back in what is
	// reported so a failure is theirs to act on rather than an ssh destination
	// to decode.
	box string
	// version is the version asked for: the one the operator named, else the
	// version local smith runs. Whether it names a release to fetch at all is
	// this operation's to settle.
	version string
	// command is the command the refusal offers its escape on, so the operator
	// is handed the verb they ran rather than one they were not using.
	command string
}

// converge settles the version and converges the box over the connection open
// returns.
//
// The version policy runs before open is called, so a build with no published
// release refuses with nothing reached and the box untouched — which is what
// lets a from-source build set a box up through every phase and fail at this
// one step alone.
//
// The connection arrives from the caller, and lazily: the setup pipeline hands
// back the one its stages already reach the box over, and a verb that was given
// a box name resolves it against the inventory first.
func (c binaryConvergence) converge(ctx context.Context, open func() (onbox.Conn, error)) (convergence, error) {
	installable := release.Installable(c.version)
	if installable == "" {
		return convergence{}, devBuildRefusal(c.version, c.command+" "+c.box)
	}
	conn, err := open()
	if err != nil {
		return convergence{}, err
	}
	result, err := onbox.NewInstaller(conn, c.box).Converge(ctx, installable)
	if err != nil {
		return convergence{}, fmt.Errorf("converge box %s to smith %s: %w", c.box, installable, err)
	}
	return convergence{Box: c.box, Result: result}, nil
}

// namedBoxConverger converges a box the operator named, to a version, and
// reports what it did. It is passed to the code that reacts to a refused relay
// rather than reached for, so an accepted prompt is driven in a test with no
// box to converge.
type namedBoxConverger func(ctx context.Context, box, version string) (convergence, error)

// convergeNamedBox returns the convergence a verb holding nothing but a box
// name runs: it resolves the name against the inventory and converges the box
// over a connection to what it resolved.
//
// Two callers run it and they are the same operation from the box's side:
// `smith machine upgrade`, and the version-skew prompt an operator accepted,
// which converges the box that refused their relayed command.
func convergeNamedBox(resolve homeResolver, exec connection.Exec) namedBoxConverger {
	return func(ctx context.Context, box, version string) (convergence, error) {
		run := binaryConvergence{box: box, version: version, command: upgradeCommand}
		return run.converge(ctx, func() (onbox.Conn, error) {
			inv, err := lookupInventory(resolve, box)
			if err != nil {
				return nil, err
			}
			return connection.New(inventory.Resolve(inv, box), exec), nil
		})
	}
}
