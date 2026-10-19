// Package gitlock reruns a git operation that lost the race to update a ref
// to another git process working in the same repo.
package gitlock

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// attempts bounds how many times Retry runs an operation.
const attempts = 8

// firstWait and longestWait bound the random wait before each rerun, which
// doubles from firstWait so processes racing for one ref spread out.
const (
	firstWait   = 10 * time.Millisecond
	longestWait = 320 * time.Millisecond
)

// Retry runs op, rerunning it after a short random wait each time git reports
// it could not lock a ref another process moved, up to a fixed number of runs.
// It returns op's last error, joined with ctx's when ctx ends while it waits.
func Retry(ctx context.Context, op func() error) error {
	wait := firstWait
	for run := 1; ; run++ {
		err := op()
		if err == nil || run == attempts || !strings.Contains(err.Error(), "cannot lock ref") {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait to rerun after %w: %w", err, ctx.Err())
		case <-time.After(rand.N(wait)):
		}
		wait = min(2*wait, longestWait)
	}
}
