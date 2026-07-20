package tailscale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byran/smith/internal/connection"
)

// Remote is the box-side command surface the box driver needs: run a remote
// command and run one with a value delivered over stdin (so the auth key never
// reaches the box's argv). connection.SSH satisfies it.
type Remote interface {
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
	RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error
}

// BoxDriver drives the tailscale box-side steps through bootstrap.sh subcommands
// over an ssh connection. It is the production Box.
type BoxDriver struct {
	remote     Remote
	scriptPath string
}

// NewBox returns a BoxDriver that drives bootstrap.sh at scriptPath on the box
// over remote (smith's ssh connection).
func NewBox(remote Remote, scriptPath string) *BoxDriver {
	return &BoxDriver{remote: remote, scriptPath: scriptPath}
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
	cmd := fmt.Sprintf("bash %s tailscale-status", d.scriptPath)
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
	cmd := fmt.Sprintf("bash %s enroll --hostname %s", d.scriptPath, shellArg(nodeName(opts.Host)))
	var out bytes.Buffer
	err := d.remote.RunWithInput(ctx, cmd, strings.NewReader(opts.AuthKey), &out, io.Discard)
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
	cmd := fmt.Sprintf("bash %s close-public-ssh", d.scriptPath)
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

// shellArg single-quotes s so it interpolates as one argument in the remote
// shell command, keeping values out of any shell-special interpretation.
func shellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
