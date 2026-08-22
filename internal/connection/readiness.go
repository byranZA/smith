package connection

import (
	"context"
	"fmt"
	"net"
	"time"
)

// sshPort is the port sshd answers on. The bootstrap-in path is plain ssh on
// the default port, so readiness is that port answering and nothing else.
const sshPort = "22"

// readinessPollInterval is how long the port-22 poll waits between dials. A
// box that is coming up answers within seconds of booting, so the cadence is
// short enough that smith reports it promptly and slow enough not to hammer a
// port that is not listening yet.
const readinessPollInterval = 5 * time.Second

// Dialer opens a connection to a network address. Only whether the address
// accepted matters, so nothing comes back but the error — and passing the
// dialer in is what lets the readiness poll be exercised against a port that
// refuses, then accepts, without a real socket.
type Dialer interface {
	// Dial reports whether address accepted a connection, returning the
	// failure when it did not.
	Dial(ctx context.Context, address string) error
}

// Clock is the passage of time. It is passed in rather than reached for, so a
// test drives the readiness poll's retries and its timeout without waiting for
// either.
type Clock interface {
	// Now reports the current time.
	Now() time.Time
	// After returns a channel that receives once d has elapsed.
	After(d time.Duration) <-chan time.Time
}

// SystemDialer returns the Dialer backed by real TCP connections.
func SystemDialer() Dialer { return systemDialer{} }

// systemDialer dials real TCP addresses, closing each connection immediately:
// the poll asks whether the port answers, never talks to what is behind it.
type systemDialer struct{}

// Dial opens a TCP connection to address and closes it again.
func (systemDialer) Dial(ctx context.Context, address string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial %s: %w", address, err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close probe connection to %s: %w", address, err)
	}
	return nil
}

// WaitForSSH dials port 22 at addr until sshd accepts a connection, giving up
// after timeout. It reports nil the moment the port answers.
//
// No provider CLI waits for sshd, so readiness is always smith's own poll: a
// box the provider calls ready refuses connections for some seconds while it
// boots. The poll is deliberately not provider-specific — a box the operator
// brought themselves becomes reachable the same way.
//
// Giving up, and being interrupted, both name the address the poll had
// reached, so a caller that knows the box id can report a box that exists and
// is being billed rather than losing it inside an error message.
func WaitForSSH(ctx context.Context, dialer Dialer, clock Clock, addr string, timeout time.Duration) error {
	address := net.JoinHostPort(addr, sshPort)
	deadline := clock.Now().Add(timeout)
	for {
		err := dialer.Dial(ctx, address)
		if err == nil {
			return nil
		}
		if !clock.Now().Before(deadline) {
			return fmt.Errorf("%s did not accept an ssh connection within %s: %w", address, timeout, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s to accept an ssh connection was interrupted: %w", address, ctx.Err())
		case <-clock.After(readinessPollInterval):
		}
	}
}
