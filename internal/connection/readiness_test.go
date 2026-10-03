package connection_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

// refusingDialer refuses a fixed number of dials before accepting, like a booting box.
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

// fakeClock elapses every wait it is asked for at once.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	fired := make(chan time.Time, 1)
	fired <- c.now
	return fired
}

func TestWaitForSSHAcceptsOnceThePortAnswers(t *testing.T) {
	t.Parallel()
	dialer := &refusingDialer{refusals: 3}

	err := connection.WaitForSSH(context.Background(), dialer, &fakeClock{}, "203.0.113.10", time.Minute)

	if err != nil {
		t.Errorf("WaitForSSH() err = %v, want the box reported reachable", err)
	}
}

func TestWaitForSSHDialsPort22AtTheBoxAddress(t *testing.T) {
	t.Parallel()
	dialer := &refusingDialer{}

	if err := connection.WaitForSSH(context.Background(), dialer, &fakeClock{}, "203.0.113.10", time.Minute); err != nil {
		t.Fatalf("WaitForSSH() err = %v, want nil", err)
	}

	want := "203.0.113.10:22"
	if len(dialer.dialed) == 0 || dialer.dialed[0] != want {
		t.Errorf("dialed %q, want %q", dialer.dialed, want)
	}
}

func TestWaitForSSHGivesUpNamingTheAddress(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := connection.WaitForSSH(ctx, &refusingDialer{refusals: 1000}, &fakeClock{}, "203.0.113.10", time.Hour)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("WaitForSSH() err = %v, want it to carry the cancellation", err)
	}
}

func TestWaitForSSHInterruptedNamesTheAddress(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := connection.WaitForSSH(ctx, &refusingDialer{refusals: 1000}, &fakeClock{}, "203.0.113.10", time.Hour)

	if err == nil || !strings.Contains(err.Error(), "203.0.113.10") {
		t.Errorf("WaitForSSH() err = %v, want it to name the address it had reached", err)
	}
}
