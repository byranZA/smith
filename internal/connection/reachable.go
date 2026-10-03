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

// Reachable reports whether smith can open a connection to the box, with a
// refused connection as (false, nil). Any other failure, such as a missing ssh
// binary, is an error because it says nothing about whether the box answers.
func Reachable(ctx context.Context, conn Runner) (bool, error) {
	if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, ErrConnect) {
			return false, nil
		}
		return false, fmt.Errorf("reachability probe: %w", err)
	}
	return true, nil
}
