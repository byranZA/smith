package status

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/tailscale"
)

// Gathered is what a Prober reads off a box: whether it was reachable, whether
// it carried a marker, the decoded marker and its schema skew, and the box's
// live facts — the full input the reconciler needs.
type Gathered struct {
	// Reachable reports whether smith could connect to the box at all.
	Reachable bool
	// MarkerPresent reports whether the box carried a marker.
	MarkerPresent bool
	// Marker is the decoded marker; the zero value when none was present.
	Marker marker.Marker
	// Skew is the marker's schema skew against this build.
	Skew marker.Skew
	// Facts are the box's live probed facts.
	Facts Facts
}

// Prober gathers a box's live facts read-only. It ships bootstrap.sh, runs its
// probe subcommand as the smith user, and — in tailscale mode — runs an
// admin-side ssh-over-tailnet probe to complete the access facts. It mutates
// nothing: probing is a query, never a remediation.
type Prober struct {
	conn  bootstrap.Conn
	admin tailscale.Admin
}

// NewProber returns a Prober that reaches the box over conn and, in tailscale
// mode, runs the tailnet reach probe over admin.
func NewProber(conn bootstrap.Conn, admin tailscale.Admin) *Prober {
	return &Prober{conn: conn, admin: admin}
}

// Gather probes the box: it checks reachability, ships and runs the read-only
// probe, decodes the marker, and — in tailscale mode with a tailnet IP — probes
// tailnet reach from the admin side. A connect failure is reported as an
// unreachable Gathered rather than a Go error; a Go error is returned only for
// unexpected infrastructure failures or a malformed marker.
func (p *Prober) Gather(ctx context.Context) (Gathered, error) {
	reachable, err := connection.Reachable(ctx, p.conn)
	if err != nil {
		return Gathered{}, fmt.Errorf("probe reachability: %w", err)
	}
	if !reachable {
		return Gathered{Reachable: false}, nil
	}

	if err := bootstrap.ShipScript(ctx, p.conn); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return Gathered{Reachable: false}, nil
		}
		return Gathered{}, fmt.Errorf("ship bootstrap script: %w", err)
	}

	var out bytes.Buffer
	cmd := fmt.Sprintf("bash %s probe", bootstrap.RemoteScriptPath)
	if err := p.conn.Run(ctx, cmd, &out, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return Gathered{Reachable: false}, nil
		}
		return Gathered{}, fmt.Errorf("run probe: %w", err)
	}

	facts, markerRaw, present, tailnetIP := parseProbe(out.String())

	g := Gathered{Reachable: true, MarkerPresent: present, Facts: facts}
	if !present {
		return g, nil
	}

	m, skew, err := marker.Decode(markerRaw)
	if err != nil {
		return Gathered{}, fmt.Errorf("decode marker: %w", err)
	}
	g.Marker, g.Skew = m, skew

	if m.AccessMode == "tailscale" && tailnetIP != "" {
		g.Facts.TailnetReach = p.probeTailnetReach(ctx, tailnetIP)
	}
	return g, nil
}

// probeTailnetReach runs the admin-side live ssh-over-tailnet probe and maps its
// result to a fact: reachable on success, denied on ErrProbeDenied, and unknown
// on any other failure so it is not silently matched.
func (p *Prober) probeTailnetReach(ctx context.Context, tailnetIP string) Fact {
	if p.admin == nil {
		return Fact{}
	}
	err := p.admin.Probe(ctx, tailnetIP)
	switch {
	case err == nil:
		return Fact{Value: "reachable", Known: true}
	case errors.Is(err, tailscale.ErrProbeDenied):
		return Fact{Value: "denied", Known: true}
	default:
		return Fact{}
	}
}

// parseProbe reads bootstrap.sh's probe output into the box's facts, the raw
// marker JSON (empty when the box carried none), whether a marker was present,
// and the box's tailnet IP when reported. A fact value of "?" decodes to an
// undeterminable Fact.
func parseProbe(output string) (facts Facts, markerRaw []byte, present bool, tailnetIP string) {
	var mb strings.Builder
	inMarker := false

	for _, line := range strings.Split(output, "\n") {
		switch {
		case line == "marker-begin":
			inMarker = true
			continue
		case line == "marker-end":
			inMarker = false
			continue
		case inMarker:
			mb.WriteString(line)
			mb.WriteByte('\n')
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		facts.assign(strings.TrimSpace(key), strings.TrimSpace(value), &tailnetIP)
	}

	raw := strings.TrimSpace(mb.String())
	if raw != "" {
		markerRaw = []byte(raw)
		present = true
	}
	return facts, markerRaw, present, tailnetIP
}

// assign sets the fact named key from its probed value. Booleans read "yes"; the
// remaining facts read into a Fact where "?" is undeterminable; tailnet-ip is
// written through to the caller's tailnetIP.
func (f *Facts) assign(key, value string, tailnetIP *string) {
	switch key {
	case "smith-user-exists":
		f.SmithUserExists = value == "yes"
	case "passwordless-sudo":
		f.PasswordlessSudo = value == "yes"
	case "ufw-active":
		f.UFWActive = fact(value)
	case "ufw-default-deny":
		f.UFWDefaultDeny = fact(value)
	case "ufw-ssh-allow":
		f.UFWSSHAllow = fact(value)
	case "permit-root-login":
		f.PermitRootLogin = fact(value)
	case "password-authentication":
		f.PasswordAuth = fact(value)
	case "fail2ban":
		f.Fail2banRunning = fact(value)
	case "auto-updates":
		f.AutoUpdates = fact(value)
	case "tailscale-running":
		f.TailscaleRunning = fact(value)
	case "tailnet-ip":
		*tailnetIP = value
	}
}

// fact turns a probed value into a Fact, treating "?" (or empty) as
// undeterminable.
func fact(value string) Fact {
	if value == "" || value == "?" {
		return Fact{}
	}
	return Fact{Value: value, Known: true}
}
