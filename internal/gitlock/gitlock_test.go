package gitlock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/gitlock"
)

// errLost is how git reports losing the race to update a ref to another process.
var errLost = errors.New("error: cannot lock ref 'refs/remotes/origin/main': is at 45e7eb3 but expected 3a9ffa9")

// losing returns a git operation that loses the race n times, then succeeds, and its count of runs.
func losing(n int) (func() error, *int) {
	runs := 0
	return func() error {
		runs++
		if runs <= n {
			return errLost
		}
		return nil
	}, &runs
}

func TestRetryRerunsAnOperationThatLostTheRaceUntilItSucceeds(t *testing.T) {
	t.Parallel()
	op, runs := losing(3)

	err := gitlock.Retry(t.Context(), op)

	if err != nil || *runs != 4 {
		t.Errorf("Retry(op losing 3 times) = %v after %d runs, want nil after 4", err, *runs)
	}
}

func TestRetryGivesUpWithTheLostRaceAfterEightRuns(t *testing.T) {
	t.Parallel()
	op, runs := losing(100)

	err := gitlock.Retry(t.Context(), op)

	if !errors.Is(err, errLost) || *runs != 8 {
		t.Errorf("Retry(op always losing) = %v after %d runs, want %v after 8", err, *runs, errLost)
	}
}

func TestRetryDoesNotRerunAnOperationThatFailedForAnotherReason(t *testing.T) {
	t.Parallel()
	errOffline := errors.New("fatal: unable to access origin: could not resolve host")
	runs := 0

	err := gitlock.Retry(t.Context(), func() error { runs++; return errOffline })

	if !errors.Is(err, errOffline) || runs != 1 {
		t.Errorf("Retry(op offline) = %v after %d runs, want %v after 1", err, runs, errOffline)
	}
}

func TestRetryStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	runs := 0

	err := gitlock.Retry(ctx, func() error { runs++; cancel(); return errLost })

	if !errors.Is(err, context.Canceled) || runs != 1 {
		t.Errorf("Retry(op losing, context cancelled) = %v after %d runs, want %v after 1", err, runs, context.Canceled)
	}
}
