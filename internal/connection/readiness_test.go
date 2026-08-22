package connection_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

// refusingDialer refuses a fixed number of connections before accepting, which
// is the shape a freshly created box has: the address answers nothing at all
// for some seconds before sshd binds to port 22.
type refusingDialer struct {
	refusals int

	dialed []string
}

func (d *refusingDialer) Dial(_ context.Context, address string) error {
	d.dialed = append(d.dialed, address)
	if len(d.dialed) <= d.refusals {
		return errors.New("connection refused")
	}
	return nil
}

// fakeClock drives the poll's retries and its timeout without waiting: every
// wait it is asked for has already elapsed.
type fakeClock struct {
	now   time.Time
	waits int
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now, c.waits = c.now.Add(d), c.waits+1
	fired := make(chan time.Time, 1)
	fired <- c.now
	return fired
}

func TestWaitForSSHAcceptsOnceThePortAnswers(t *testing.T) {
	dialer := &refusingDialer{refusals: 3}

	err := connection.WaitForSSH(context.Background(), dialer, &fakeClock{}, "203.0.113.10", time.Minute)

	if err != nil {
		t.Fatalf("WaitForSSH() err = %v, want the box reported reachable", err)
	}
	if len(dialer.dialed) != 4 {
		t.Errorf("dialed %d times, want the poll to retry until the port answered", len(dialer.dialed))
	}
}

func TestWaitForSSHDialsPort22AtTheBoxAddress(t *testing.T) {
	dialer := &refusingDialer{}

	if err := connection.WaitForSSH(context.Background(), dialer, &fakeClock{}, "203.0.113.10", time.Minute); err != nil {
		t.Fatalf("WaitForSSH() err = %v", err)
	}

	want := "203.0.113.10:22"
	if len(dialer.dialed) == 0 || dialer.dialed[0] != want {
		t.Errorf("dialed %q, want %q", dialer.dialed, want)
	}
}

func TestWaitForSSHGivesUpNamingTheAddress(t *testing.T) {
	dialer := &refusingDialer{refusals: 1000}

	err := connection.WaitForSSH(context.Background(), dialer, &fakeClock{}, "203.0.113.10", time.Minute)

	if err == nil {
		t.Fatal("WaitForSSH() err = nil, want a port that never accepts reported")
	}
	if !strings.Contains(err.Error(), "203.0.113.10") {
		t.Errorf("WaitForSSH() err = %v, want it to name the address", err)
	}
}

func TestWaitForSSHStopsWhenTheOperatorInterrupts(t *testing.T) {
	dialer := &refusingDialer{refusals: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := connection.WaitForSSH(ctx, dialer, &fakeClock{}, "203.0.113.10", time.Hour)

	if err == nil {
		t.Fatal("WaitForSSH() err = nil, want an interrupted poll reported")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WaitForSSH() err = %v, want it to carry the cancellation", err)
	}
	if !strings.Contains(err.Error(), "203.0.113.10") {
		t.Errorf("WaitForSSH() err = %v, want it to name the address it had reached", err)
	}
}
