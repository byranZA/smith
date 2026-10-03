package tailscale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/shipped"
)

// ShippedScript is the shipped bootstrap.sh a BoxDriver drives by subcommand:
// it ships on its first run, follows the box to a new address on Move, and is
// removed again on Close over the address it reaches the box at then.
type ShippedScript interface {
	Run(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error
	RunWithInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, sub string, args ...string) error
	Move(conn shipped.Conn)
	Close(ctx context.Context)
}

// BoxDriver drives the tailscale box-side steps through bootstrap.sh
// subcommands over ssh. It is the production Box.
//
// It owns one shipped bootstrap.sh for one access stage and one login, which
// reaches the box at the public host until MoveToTailnet and at the tailnet
// address after it — which is where Close then removes the copy from.
type BoxDriver struct {
	script ShippedScript
	dial   func(host string) shipped.Conn
}

// NewBox returns a BoxDriver that drives script, already set to reach the box
// at the public host the run came in over. dial returns the connection to the
// box at a host, as the login every stage travels as; the driver dials the
// tailnet address with it once it is moved there.
func NewBox(script ShippedScript, dial func(host string) shipped.Conn) *BoxDriver {
	return &BoxDriver{script: script, dial: dial}
}

// MoveToTailnet sends every later call — including Close — to the box at
// tailnetIP. Call it only once a live probe has proven that address reaches
// the box: the public host stops answering once public SSH is closed.
func (d *BoxDriver) MoveToTailnet(tailnetIP string) {
	d.script.Move(d.dial(tailnetIP))
}

// Close removes the driver's shipped bootstrap.sh from the box over the address
// it reaches the box at now, best effort. It never fails.
func (d *BoxDriver) Close(ctx context.Context) {
	d.script.Close(ctx)
}

// enrolledIPPrefix is the line bootstrap.sh's enroll and tailscale-status
// subcommands print to report the box's tailnet IP once it reaches Running.
const enrolledIPPrefix = "tailscale-ip="

// CurrentIP runs bootstrap.sh's tailscale-status subcommand, which prints the
// box's tailnet IP only when the node is already enrolled and Running. An
// un-enrolled box prints nothing, so this returns "" and the access layer
// enrolls; a connect failure is surfaced so a re-run is not mistaken for a fresh
// box. It reads state only — it never mutates the box.
func (d *BoxDriver) CurrentIP(ctx context.Context) (string, error) {
	var out bytes.Buffer
	if err := d.script.Run(ctx, &out, io.Discard, "tailscale-status"); err != nil {
		return "", fmt.Errorf("read tailscale status: %w", err)
	}
	return parseEnrolledIP(out.String()), nil
}

// Enroll runs bootstrap.sh's enroll subcommand with the auth key on stdin and
// parses the reported tailnet IP. A non-connect failure means the node never
// reached Running, reported as ErrEnrollNotRunning so the caller can attribute
// the missing tagOwners prerequisite.
func (d *BoxDriver) Enroll(ctx context.Context, opts EnrollOptions) (string, error) {
	var out bytes.Buffer
	err := d.script.RunWithInput(ctx, strings.NewReader(opts.AuthKey), &out, io.Discard, "enroll", "--hostname", nodeName(opts.Host))
	if err != nil {
		if errors.Is(err, connection.ErrConnect) || errors.Is(err, shipped.ErrNotShipped) {
			return "", fmt.Errorf("enroll: %w", err)
		}
		return "", fmt.Errorf("%w: %w", ErrEnrollNotRunning, err)
	}
	ip := parseEnrolledIP(out.String())
	if ip == "" {
		return "", fmt.Errorf("%w: enroll reported no tailnet IP", ErrEnrollNotRunning)
	}
	return ip, nil
}

// ClosePublicSSH runs bootstrap.sh's close-public-ssh subcommand, which closes
// public port 22 and records the access phase as complete in the marker.
func (d *BoxDriver) ClosePublicSSH(ctx context.Context) error {
	if err := d.script.Run(ctx, io.Discard, io.Discard, "close-public-ssh"); err != nil {
		return fmt.Errorf("close public ssh: %w", err)
	}
	return nil
}

// parseEnrolledIP extracts the tailnet IP from the enroll subcommand's output,
// reading the last tailscale-ip= line it printed.
func parseEnrolledIP(output string) string {
	var ip string
	for _, line := range strings.Split(output, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), enrolledIPPrefix); ok {
			ip = strings.TrimSpace(v)
		}
	}
	return ip
}
