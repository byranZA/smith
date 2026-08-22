package connection

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Runner is the narrow slice of a connection the reachability probe needs: run
// a remote command. *SSH satisfies it, and so does any fake a caller passes in.
type Runner interface {
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
}

// Reachable reports whether smith can open a connection to the box: it runs
// the cheapest command there is and looks only at whether it got in.
//
// A refused connection is (false, nil), not an error — an unreachable box is a
// fact about the box, and a caller deciding what to do about it should not
// have to unwrap an error to learn it. Anything else — a missing ssh binary, a
// command that ran and failed — is a Go error, because it says nothing about
// whether the box answers.
func Reachable(ctx context.Context, conn Runner) (bool, error) {
	if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, ErrConnect) {
			return false, nil
		}
		return false, fmt.Errorf("reachability probe: %w", err)
	}
	return true, nil
}
