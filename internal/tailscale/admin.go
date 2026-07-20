package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/byran/smith/internal/connection"
)

// AdminDriver reads the admin machine's own tailnet state and runs the live
// ssh-over-tailnet probe, both via local commands. It is the production Admin.
type AdminDriver struct {
	exec connection.Exec
}

// NewAdmin returns an AdminDriver that runs local tailscale/ssh commands through
// exec. Pass connection.System() for the real binaries.
func NewAdmin(exec connection.Exec) *AdminDriver {
	return &AdminDriver{exec: exec}
}

// Status runs `tailscale status --json` on the admin machine and reports whether
// this machine is a Running tailnet member and the operator's tailnet identity.
func (a *AdminDriver) Status(ctx context.Context) (AdminStatus, error) {
	var out bytes.Buffer
	if err := a.exec.Run(ctx, "tailscale", []string{"status", "--json"}, nil, &out, io.Discard); err != nil {
		return AdminStatus{}, fmt.Errorf("tailscale status: %w", err)
	}
	status, err := parseAdminStatus(out.Bytes())
	if err != nil {
		return AdminStatus{}, fmt.Errorf("parse tailscale status: %w", err)
	}
	return status, nil
}

// Probe runs `ssh smith@<tailnetIP> true` over the tailnet through the canonical
// connection boundary, which owns the hardened ssh option set and ErrConnect
// classification. A ran-and-denied result is reported as ErrProbeDenied so the
// caller can attribute the missing ssh ACL prerequisite; a genuine connect
// failure is left as connection.ErrConnect so it is not misattributed to it.
func (a *AdminDriver) Probe(ctx context.Context, tailnetIP string) error {
	conn := connection.New("smith@"+tailnetIP, a.exec)
	if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return fmt.Errorf("probe smith@%s over the tailnet: %w", tailnetIP, err)
		}
		return fmt.Errorf("%w: %w", ErrProbeDenied, err)
	}
	return nil
}

// tailscaleStatus is the slice of `tailscale status --json` smith reads: the
// backend state, and the current user's identity resolved through the User map.
// Fields serialize by Go name (tailscale's ipnstate has no json tags).
type tailscaleStatus struct {
	BackendState string
	Self         *struct {
		UserID int64
	}
	User map[string]struct {
		LoginName string
	}
}

// parseAdminStatus decodes `tailscale status --json` into an AdminStatus: on the
// tailnet when BackendState is "Running", with the operator's identity taken
// from the User entry the Self peer belongs to.
func parseAdminStatus(data []byte) (AdminStatus, error) {
	var s tailscaleStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return AdminStatus{}, fmt.Errorf("decode status json: %w", err)
	}
	status := AdminStatus{OnTailnet: s.BackendState == "Running"}
	if s.Self != nil {
		if u, ok := s.User[strconv.FormatInt(s.Self.UserID, 10)]; ok {
			status.Identity = u.LoginName
		}
	}
	return status, nil
}
