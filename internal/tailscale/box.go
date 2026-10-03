package tailscale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
)

// Remote is the box-side command surface the box driver needs at one address:
// ship a file, run a remote command, and run one with a value delivered over
// stdin (so the auth key never reaches the box's argv). connection.SSH
// satisfies it.
type Remote interface {
	Copy(ctx context.Context, localPath, remotePath string) error
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
	RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error
}

// BoxDriver drives the tailscale box-side steps through bootstrap.sh
// subcommands over ssh. It is the production Box.
//
// It ships its own copy of bootstrap.sh the first time a step needs it, so it
// owns that copy for one access stage and one login, and Close removes it. It
// reaches the box at the public host until MoveToTailnet, and at the tailnet
// address after it — which is where Close then removes the copy from.
type BoxDriver struct {
	dial    func(host string) Remote
	remote  Remote
	shipped *bootstrap.Shipped
}

// NewBox returns a BoxDriver that reaches the box at host, the public host the
// run came in over, through dial. dial returns the connection to the box at a
// host, as the login every stage travels as; the driver dials it again with
// the tailnet address once it is moved there.
func NewBox(dial func(host string) Remote, host string) *BoxDriver {
	return &BoxDriver{dial: dial, remote: dial(host)}
}

// MoveToTailnet sends every later call — including Close — to the box at
// tailnetIP. Call it only once a live probe has proven that address reaches
// the box: the public host stops answering once public SSH is closed.
func (d *BoxDriver) MoveToTailnet(tailnetIP string) {
	d.remote = d.dial(tailnetIP)
	if d.shipped != nil {
		moved := d.shipped.Over(d.remote)
		d.shipped = &moved
	}
}

// Close removes the driver's shipped bootstrap.sh from the box over the address
// it reaches the box at now, best effort, and does nothing when nothing was
// shipped. It never fails: see bootstrap.Shipped.Remove.
func (d *BoxDriver) Close(ctx context.Context) {
	if d.shipped == nil {
		return
	}
	d.shipped.Remove(ctx)
	d.shipped = nil
}

// script ships bootstrap.sh on first use and returns the remote path every
// later step reuses.
func (d *BoxDriver) script(ctx context.Context) (string, error) {
	if d.shipped == nil {
		shipped, err := bootstrap.Ship(ctx, d.remote, bootstrap.Script)
		if err != nil {
			return "", fmt.Errorf("ship bootstrap script: %w", err)
		}
		d.shipped = &shipped
	}
	return d.shipped.Path(), nil
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
	script, err := d.script(ctx)
	if err != nil {
		return "", err
	}
	cmd := fmt.Sprintf("bash %s tailscale-status", connection.ShellArg(script))
	var out bytes.Buffer
	if err := d.remote.Run(ctx, cmd, &out, io.Discard); err != nil {
		return "", fmt.Errorf("run tailscale-status: %w", err)
	}
	return parseEnrolledIP(out.String()), nil
}

// Enroll runs bootstrap.sh's enroll subcommand with the auth key on stdin and
// parses the reported tailnet IP. A non-connect failure means the node never
// reached Running, reported as ErrEnrollNotRunning so the caller can attribute
// the missing tagOwners prerequisite.
func (d *BoxDriver) Enroll(ctx context.Context, opts EnrollOptions) (string, error) {
	script, err := d.script(ctx)
	if err != nil {
		return "", err
	}
	cmd := fmt.Sprintf("bash %s enroll --hostname %s", connection.ShellArg(script), connection.ShellArg(nodeName(opts.Host)))
	var out bytes.Buffer
	err = d.remote.RunWithInput(ctx, cmd, strings.NewReader(opts.AuthKey), &out, io.Discard)
	if err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return "", fmt.Errorf("run enroll: %w", err)
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
	script, err := d.script(ctx)
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("bash %s close-public-ssh", connection.ShellArg(script))
	if err := d.remote.Run(ctx, cmd, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("run close-public-ssh: %w", err)
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
