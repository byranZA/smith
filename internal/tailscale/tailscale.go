// Package tailscale drives smith's --access=tailscale layer: it joins the box
// to the operator's tailnet as a keyless Tailscale SSH node tagged tag:smith,
// then closes public SSH — but only after a live ssh-over-tailnet probe from the
// admin machine proves the new door opens.
//
// The reach is Tailscale SSH on a tag:smith node (see ADR-0001): Tailscale is
// the identity authority, so no authorized_keys manage tailnet logins. Tagging
// pulls in two one-time-per-tailnet prerequisites the enrollment auth key cannot
// self-serve — a tagOwners entry for tag:smith and an ssh ACL rule granting the
// operator smith access to tag:smith — which fail at different stages: a missing
// tagOwners entry stops enrollment from ever reaching Running, and a missing ssh
// rule lets the box enroll but denies the tailnet probe. smith prints both up
// front, personalized, and detects them reactively.
//
// The verify order is the lock-out-safety invariant made concrete:
// local-on-tailnet → enroll → Running+IP → live ssh probe → only then close
// public SSH. Any failure leaves public SSH open and reports which prerequisite
// to fix. This package owns that state machine; the box-side and admin-side
// command surfaces are injected as narrow interfaces so it is unit-testable with
// faked tailscale/ssh outputs, never a real tailnet.
package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrNotOnTailnet is returned when the admin machine running smith is not itself
// a member of the tailnet, so smith cannot verify tailnet reachability and
// refuses --access=tailscale before mutating the box.
var ErrNotOnTailnet = errors.New("admin machine is not on the tailnet")

// ErrEnrollNotRunning is returned when the box enrolls but never reaches the
// Running state. Its cause is the missing tagOwners prerequisite: an auth key
// carrying a tag its tailnet does not own cannot bring the node up.
var ErrEnrollNotRunning = errors.New("tailscale enrollment did not reach Running")

// ErrProbeDenied is returned when the box enrolled and received a tailnet IP but
// the live ssh-over-tailnet probe is denied. Its cause is the missing ssh ACL
// rule granting the operator smith access to tag:smith.
var ErrProbeDenied = errors.New("tailnet ssh probe was denied")

// PublicSSH is the firewall target for public port 22, derived from the access
// mode and the door smith connected over.
type PublicSSH int

const (
	// PublicSSHOpen keeps hardened public SSH on port 22 reachable.
	PublicSSHOpen PublicSSH = iota
	// PublicSSHClosed closes public port 22 so the box is reached only over the
	// tailnet.
	PublicSSHClosed
)

// String renders the firewall target as the token bootstrap.sh's firewall phase
// consumes.
func (p PublicSSH) String() string {
	if p == PublicSSHClosed {
		return "closed"
	}
	return "open"
}

// EnrollOptions carries what enrolling the box needs: the box's host (for the
// smith-<host> tailnet hostname) and the resolved auth key delivered to the box
// over stdin.
type EnrollOptions struct {
	// Host is the box's host, used to name the node smith-<host>.
	Host string
	// AuthKey is the resolved, single-use tailnet auth key.
	AuthKey string
}

// Box is the box-side surface the tailscale layer drives over smith's ssh
// connection: enroll the node and, as the final mutating step, close public SSH.
type Box interface {
	// Enroll runs tailscale up on the box with the auth key over stdin and
	// returns the box's tailnet IP once it reaches Running. A box that never
	// reaches Running is reported as ErrEnrollNotRunning.
	Enroll(ctx context.Context, opts EnrollOptions) (tailnetIP string, err error)
	// ClosePublicSSH closes public port 22 on the box — the access layer's last
	// mutating step, gated on a proven tailnet probe.
	ClosePublicSSH(ctx context.Context) error
}

// AdminStatus is what the admin machine's tailscale status reports: whether this
// machine is on the tailnet and, if so, the operator's tailnet identity.
type AdminStatus struct {
	// OnTailnet reports whether the admin machine is a Running tailnet member.
	OnTailnet bool
	// Identity is the operator's tailnet login (e.g. an email), used to
	// personalize the printed prerequisites.
	Identity string
}

// Admin is the admin machine's own tailscale/ssh surface: read this machine's
// tailnet membership and run the live ssh-over-tailnet probe.
type Admin interface {
	// Status reports whether the admin machine is on the tailnet and the
	// operator's tailnet identity.
	Status(ctx context.Context) (AdminStatus, error)
	// Probe runs a live ssh smith@<tailnetIP> over the tailnet. A denied probe
	// is reported as ErrProbeDenied.
	Probe(ctx context.Context, tailnetIP string) error
}

// EstablishOptions carries the parameters the enroll-then-close sequence needs.
type EstablishOptions struct {
	// Host is the box's host — names the node smith-<host> and forms the tailnet
	// name the operator re-runs over.
	Host string
	// AuthKey is the resolved tailnet auth key delivered to the box over stdin.
	AuthKey string
}

// Result reports a successful establish: the box's tailnet IP, that public SSH
// was closed, and the tailnet name the operator should re-run over.
type Result struct {
	// TailnetIP is the box's 100.x tailnet address.
	TailnetIP string
	// PublicSSHClosed reports that public port 22 was closed after the probe.
	PublicSSHClosed bool
	// ReRunHost is the box's tailnet name — the host to pass on a re-run.
	ReRunHost string
}

// Access orchestrates the tailscale access layer over its injected box-side and
// admin-side surfaces.
type Access struct {
	box   Box
	admin Admin
}

// NewAccess returns an Access that enrolls over box and verifies over admin.
func NewAccess(box Box, admin Admin) *Access {
	return &Access{box: box, admin: admin}
}

// CheckAdminOnTailnet refuses --access=tailscale up front, before anything on
// the box is mutated, when the admin machine running smith is not itself on the
// tailnet — smith could not then verify tailnet reachability, so it must not
// proceed to enroll and risk closing public SSH blind.
func CheckAdminOnTailnet(ctx context.Context, admin Admin) error {
	status, err := admin.Status(ctx)
	if err != nil {
		return fmt.Errorf("read admin tailnet status: %w", err)
	}
	if !status.OnTailnet {
		return fmt.Errorf("cannot verify tailnet reachability: %w", ErrNotOnTailnet)
	}
	return nil
}

// Establish runs the reachability-gated verify order after the base layer has
// left the box provisioned and secured: enroll the tag:smith node, prove it is
// reachable with a live ssh-over-tailnet probe, and only then close public SSH.
// A failure at enrollment attributes the missing tagOwners prerequisite; a
// denied probe attributes the missing ssh ACL prerequisite; both leave public
// SSH open. Public SSH is never closed unless the probe succeeds first.
func (a *Access) Establish(ctx context.Context, opts EstablishOptions) (Result, error) {
	ip, err := a.box.Enroll(ctx, EnrollOptions(opts))
	if err != nil {
		if errors.Is(err, ErrEnrollNotRunning) {
			return Result{}, fmt.Errorf("enroll box: %w; add a tagOwners entry for tag:smith to the tailnet policy, then re-run (public SSH left open)", err)
		}
		return Result{}, fmt.Errorf("enroll box: %w", err)
	}

	if err := a.admin.Probe(ctx, ip); err != nil {
		if errors.Is(err, ErrProbeDenied) {
			return Result{}, fmt.Errorf("probe %s over the tailnet: %w; add an ssh ACL rule granting smith access to tag:smith (action accept), then re-run (public SSH left open)", ip, err)
		}
		return Result{}, fmt.Errorf("probe %s over the tailnet: %w", ip, err)
	}

	if err := a.box.ClosePublicSSH(ctx); err != nil {
		return Result{}, fmt.Errorf("close public ssh: %w", err)
	}

	return Result{TailnetIP: ip, PublicSSHClosed: true, ReRunHost: nodeName(opts.Host)}, nil
}

// nodeName is the tailnet hostname smith advertises for a box at host: smith-<host>.
func nodeName(host string) string {
	return "smith-" + host
}

// Prereqs renders the two one-time-per-tailnet prerequisites the enrollment auth
// key cannot self-serve, personalized to the operator's tailnet identity. They
// are printed up front so the operator can add them before enrollment fails on
// them reactively.
func Prereqs(identity, host string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--access=tailscale needs two one-time-per-tailnet policy entries smith cannot add for you\n")
	fmt.Fprintf(&b, "(your enrollment auth key can join a node, but not edit the tailnet policy):\n\n")
	fmt.Fprintf(&b, "  1. tagOwners — declare tag:smith so the node's key never expires, e.g.\n")
	fmt.Fprintf(&b, "       \"tagOwners\": { \"tag:smith\": [\"autogroup:admin\"] }\n")
	fmt.Fprintf(&b, "     Missing this, enrollment of %s never reaches Running.\n\n", nodeName(host))
	fmt.Fprintf(&b, "  2. an ssh ACL rule granting %s access to tag:smith (action accept, not check), e.g.\n", identity)
	fmt.Fprintf(&b, "       { \"action\": \"accept\", \"src\": [%q], \"dst\": [\"tag:smith\"], \"users\": [\"smith\"] }\n", identity)
	fmt.Fprintf(&b, "     Missing this, the box enrolls but the tailnet ssh probe is denied.\n\n")
	return b.String()
}

// PublicSSHTarget derives the firewall target for public port 22 from the access
// mode and whether smith connected over the tailnet. Public mode keeps 22 open.
// Tailscale mode keeps 22 open while smith is still bootstrapping over public
// (the base layer must not sever its own reach; Establish closes 22 only after
// the probe), and keeps 22 closed on a re-run reached over the tailnet — so a
// tailnet re-run never transiently re-opens public 22.
func PublicSSHTarget(accessMode string, connectedOverTailnet bool) PublicSSH {
	if accessMode == "tailscale" && connectedOverTailnet {
		return PublicSSHClosed
	}
	return PublicSSHOpen
}

// tailnetCGNAT is the 100.64.0.0/10 carrier-grade NAT range Tailscale assigns
// tailnet IPs from; a connection whose source is in it arrived over the tailnet.
var tailnetCGNAT = netip.MustParsePrefix("100.64.0.0/10")

// ConnectedOverTailnet reports whether an ssh session described by the box's
// SSH_CONNECTION value ("clientip clientport serverip serverport") arrived over
// the tailnet, i.e. its client source address is in Tailscale's CGNAT range.
func ConnectedOverTailnet(sshConnection string) bool {
	fields := strings.Fields(sshConnection)
	if len(fields) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(fields[0])
	if err != nil {
		return false
	}
	return tailnetCGNAT.Contains(addr)
}
