// Package status is the read-only, state-oriented half of smith's setup/status
// seam: `smith machine status <host>` reads the on-box marker, re-probes the
// box's live facts, and reports how the box has drifted from what setup
// established — without ever changing it.
//
// The deep module here is the drift reconciler: (marker, live facts) → one of
// five ordered verdicts — unreachable → never-bootstrapped → partially-
// bootstrapped → matches → drifted. Reconciliation is per-fact, grouped under
// the phase that owns it (smith-user, firewall, ssh-hardening, fail2ban,
// auto-updates, and — in tailscale mode — access). An undeterminable fact gets
// its own `?` marker and folds into the drifted verdict rather than silently
// matching; lost passwordless sudo is itself the drifted fact and marks the
// root-probes it blocks unverifiable. Read-only is a structural invariant: this
// package only queries, it never remediates — `smith machine setup` is what
// converges a drifted box.
package status

import (
	"fmt"
	"strings"

	"github.com/byran/smith/internal/marker"
)

// expectedPhases is the full base-layer phase set a fully-provisioned box
// records, in order. The access phase completes it in both modes (public via
// phase_access, tailscale via close-public-ssh). A marker missing any of these
// is partially bootstrapped, not merely drifted.
var expectedPhases = []string{
	"packages", "smith-user", "smith-keys", "firewall",
	"ssh-hardening", "fail2ban", "auto-updates", "access",
}

// Fact is one probed value that may be undeterminable. Known is false when smith
// could not determine the value at all — the `?` marker that folds into a
// drifted verdict rather than silently matching.
type Fact struct {
	// Value is the probed value (e.g. "no", "active", "running").
	Value string
	// Known reports whether the value could be determined.
	Known bool
}

// Facts are a box's live, read-only facts, grouped by the phase that owns them.
// SmithUserExists and PasswordlessSudo are always determinable (no root needed);
// the remaining facts need root, so when PasswordlessSudo is false the reconciler
// marks them unverifiable rather than trusting a `?`. The tailscale-mode access
// facts are ignored in public mode.
type Facts struct {
	// SmithUserExists reports whether the smith user is present on the box.
	SmithUserExists bool
	// PasswordlessSudo reports whether the smith user still holds passwordless
	// sudo. When false, it is the drifted fact and gates the root-only probes.
	PasswordlessSudo bool
	// UFWActive is whether ufw is active ("active"/"inactive").
	UFWActive Fact
	// UFWDefaultDeny is ufw's default inbound policy ("deny"/"allow").
	UFWDefaultDeny Fact
	// UFWSSHAllow is whether public port 22 is allowed ("allow"/"deny"); its
	// expected value follows the access mode.
	UFWSSHAllow Fact
	// PermitRootLogin is sshd's effective PermitRootLogin, read from `sshd -T`.
	PermitRootLogin Fact
	// PasswordAuth is sshd's effective PasswordAuthentication, read from `sshd -T`.
	PasswordAuth Fact
	// Fail2banRunning is whether the fail2ban service runs ("running"/"stopped").
	Fail2banRunning Fact
	// AutoUpdates is whether unattended security upgrades are on ("enabled"/"disabled").
	AutoUpdates Fact
	// TailscaleRunning is the box's Tailscale backend state ("running"/"stopped"),
	// probed in tailscale mode.
	TailscaleRunning Fact
	// TailnetReach is the admin-side live ssh-over-tailnet probe result
	// ("reachable"/"denied"), probed in tailscale mode.
	TailnetReach Fact
}

// Verdict is one of the five ordered status outcomes, from most to least severe
// in reach terms: unreachable, never-bootstrapped, partially-bootstrapped,
// matches, drifted.
type Verdict int

const (
	// VerdictUnreachable means smith could not connect to the box.
	VerdictUnreachable Verdict = iota
	// VerdictNeverBootstrapped means the box carries no marker — setup never ran.
	VerdictNeverBootstrapped
	// VerdictPartial means the marker records only some phases — setup ran but did
	// not complete. Kept separate from drift.
	VerdictPartial
	// VerdictMatches means every probed fact matches the marker.
	VerdictMatches
	// VerdictDrifted means at least one fact drifted, is undeterminable, or is
	// unverifiable.
	VerdictDrifted
)

// ExitCode maps a verdict to smith's process exit code: 0 matches, 1 drifted,
// 2 not-provisioned (never or partial), 3 unreachable.
func (v Verdict) ExitCode() int {
	switch v {
	case VerdictMatches:
		return 0
	case VerdictDrifted:
		return 1
	case VerdictNeverBootstrapped, VerdictPartial:
		return 2
	case VerdictUnreachable:
		return 3
	default:
		return 1
	}
}

// FindingStatus is how one probed fact reconciled against its expected value.
type FindingStatus int

const (
	// StatusMatch means the actual value equals the expected value.
	StatusMatch FindingStatus = iota
	// StatusDrift means the actual value differs from the expected value.
	StatusDrift
	// StatusUnknown means the fact could not be determined (the `?` marker).
	StatusUnknown
	// StatusUnverifiable means the probe needed root that is no longer available,
	// so the fact could not be checked — a consequence of lost passwordless sudo.
	StatusUnverifiable
)

// Finding is one probed fact reconciled against what setup established.
type Finding struct {
	// Fact is the human-readable fact name.
	Fact string
	// Expected is the value setup established.
	Expected string
	// Actual is the probed value (or a marker such as "?" / "unverifiable").
	Actual string
	// Status is how Actual reconciled against Expected.
	Status FindingStatus
}

// Group is the set of findings owned by one phase.
type Group struct {
	// Phase is the phase that owns these findings, in the marker's vocabulary.
	Phase string
	// Findings are the phase's reconciled facts.
	Findings []Finding
}

// Report is the outcome of reconciling a box: the verdict, any marker schema
// note, and — for matches/drifted — the per-phase findings, or — for partial —
// the phases still missing.
type Report struct {
	// Verdict is the reconciled outcome.
	Verdict Verdict
	// Skew is the marker's schema skew against this build; its note is surfaced.
	Skew marker.Skew
	// AccessMode is the box's recorded access mode ("public"/"tailscale").
	AccessMode string
	// Host is the box smith could not reach; set only for an unreachable verdict.
	Host string
	// Groups are the per-phase findings for a matches/drifted verdict.
	Groups []Group
	// MissingPhases are the unrecorded phases for a partial verdict.
	MissingPhases []string
}

// ExitCode is the process exit code for the report's verdict.
func (r Report) ExitCode() int { return r.Verdict.ExitCode() }

// Unreachable builds the report for a box smith could not connect to.
func Unreachable(host string) Report {
	return Report{Verdict: VerdictUnreachable, Host: host}
}

// Reconcile compares a decoded marker (present reports whether the box carried
// one) and the box's live facts, producing the report. It applies the five
// ordered verdicts: a missing marker is never-bootstrapped; an incomplete phase
// set is partial (kept separate from drift); otherwise it reconciles each fact
// under its owning phase and is drifted if any fact drifted, was undeterminable,
// or was unverifiable, else matches. It never mutates anything.
func Reconcile(m marker.Marker, skew marker.Skew, present bool, facts Facts) Report {
	if !present {
		return Report{Verdict: VerdictNeverBootstrapped, Skew: skew}
	}
	if missing := missingPhases(m.CompletedPhases); len(missing) > 0 {
		return Report{Verdict: VerdictPartial, Skew: skew, AccessMode: m.AccessMode, MissingPhases: missing}
	}
	groups := reconcileFacts(m.AccessMode, facts)
	return Report{Verdict: verdictFromGroups(groups), Skew: skew, AccessMode: m.AccessMode, Groups: groups}
}

// missingPhases returns the expected base-layer phases absent from completed, in
// order.
func missingPhases(completed []string) []string {
	have := make(map[string]bool, len(completed))
	for _, p := range completed {
		have[p] = true
	}
	var missing []string
	for _, p := range expectedPhases {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	return missing
}

// reconcileFacts reconciles every live fact under its owning phase. Root-only
// facts are gated on passwordless sudo: when it is lost they are unverifiable
// rather than compared. The access group is present only in tailscale mode.
func reconcileFacts(accessMode string, f Facts) []Group {
	sudoLost := !f.PasswordlessSudo

	groups := []Group{
		{Phase: "smith-user", Findings: []Finding{
			boolFinding("smith user", "present", "absent", f.SmithUserExists),
			boolFinding("passwordless sudo", "present", "absent", f.PasswordlessSudo),
		}},
		{Phase: "firewall", Findings: []Finding{
			rootFinding("ufw active", "active", f.UFWActive, sudoLost),
			rootFinding("ufw default-deny incoming", "deny", f.UFWDefaultDeny, sudoLost),
			rootFinding("public SSH (port 22)", expectedPublicSSH(accessMode), f.UFWSSHAllow, sudoLost),
		}},
		{Phase: "ssh-hardening", Findings: []Finding{
			rootFinding("PermitRootLogin", "no", f.PermitRootLogin, sudoLost),
			rootFinding("PasswordAuthentication", "no", f.PasswordAuth, sudoLost),
		}},
		{Phase: "fail2ban", Findings: []Finding{
			rootFinding("fail2ban service", "running", f.Fail2banRunning, sudoLost),
		}},
		{Phase: "auto-updates", Findings: []Finding{
			rootFinding("unattended security upgrades", "enabled", f.AutoUpdates, sudoLost),
		}},
	}

	if accessMode == "tailscale" {
		groups = append(groups, Group{Phase: "access", Findings: []Finding{
			// The Tailscale backend state and the tailnet ssh probe do not need
			// root, so they are checked even when passwordless sudo is lost.
			rootFinding("tailscale node", "running", f.TailscaleRunning, false),
			rootFinding("tailnet ssh reach", "reachable", f.TailnetReach, false),
		}})
	}
	return groups
}

// expectedPublicSSH is the expected public-22 firewall state for the access
// mode: public keeps 22 open (allow), tailscale closes it (deny).
func expectedPublicSSH(accessMode string) string {
	if accessMode == "tailscale" {
		return "deny"
	}
	return "allow"
}

// boolFinding reconciles an always-determinable boolean fact whose expected
// state is true (present).
func boolFinding(name, whenTrue, whenFalse string, actual bool) Finding {
	if actual {
		return Finding{Fact: name, Expected: whenTrue, Actual: whenTrue, Status: StatusMatch}
	}
	return Finding{Fact: name, Expected: whenTrue, Actual: whenFalse, Status: StatusDrift}
}

// rootFinding reconciles a probed fact against expected. When sudoLost is set the
// probe could not run at all, so the fact is unverifiable; an undeterminable fact
// is unknown; otherwise it matches or drifts.
func rootFinding(name, expected string, actual Fact, sudoLost bool) Finding {
	switch {
	case sudoLost:
		return Finding{Fact: name, Expected: expected, Actual: "unverifiable (sudo unavailable)", Status: StatusUnverifiable}
	case !actual.Known:
		return Finding{Fact: name, Expected: expected, Actual: "?", Status: StatusUnknown}
	case actual.Value == expected:
		return Finding{Fact: name, Expected: expected, Actual: actual.Value, Status: StatusMatch}
	default:
		return Finding{Fact: name, Expected: expected, Actual: actual.Value, Status: StatusDrift}
	}
}

// verdictFromGroups is Matches only when every finding matched; any drift,
// unknown, or unverifiable fact folds into Drifted.
func verdictFromGroups(groups []Group) Verdict {
	for _, g := range groups {
		for _, f := range g.Findings {
			if f.Status != StatusMatch {
				return VerdictDrifted
			}
		}
	}
	return VerdictMatches
}

// String renders the human-readable status report: a verdict headline, any
// marker schema note, the per-phase findings (or missing phases), and — for a
// non-clean verdict — the re-run remedy. It is the query-only counterpart to the
// setup failure report.
func (r Report) String() string {
	var b strings.Builder

	switch r.Verdict {
	case VerdictUnreachable:
		fmt.Fprintf(&b, "✗ unreachable: could not connect to %s\n", r.Host)
		return b.String()
	case VerdictNeverBootstrapped:
		b.WriteString("✗ never provisioned: no smith marker on the box\n")
		writeNote(&b, r.Skew)
		b.WriteString("\n  Run `smith machine setup <login>@<host>` to provision it.\n")
		return b.String()
	case VerdictPartial:
		b.WriteString("✗ partially provisioned: setup did not complete\n")
		writeNote(&b, r.Skew)
		fmt.Fprintf(&b, "\n  missing phases: %s\n", strings.Join(r.MissingPhases, ", "))
		b.WriteString("\n  Re-run `smith machine setup <login>@<host>` to finish — the re-run resumes.\n")
		return b.String()
	case VerdictMatches:
		b.WriteString("✓ matches: the box matches what setup established\n")
	default: // VerdictDrifted
		b.WriteString("✗ drifted: the box no longer matches what setup established\n")
	}
	writeNote(&b, r.Skew)

	for _, g := range r.Groups {
		fmt.Fprintf(&b, "\n%s\n", g.Phase)
		for _, f := range g.Findings {
			b.WriteString("  " + f.line() + "\n")
		}
	}

	if r.Verdict == VerdictDrifted {
		b.WriteString("\n  Re-run `smith machine setup <login>@<host>` to converge — status only reports, it never changes the box.\n")
	}
	return b.String()
}

// line renders one finding: a status glyph, the fact, and — for a non-match —
// the expected-versus-actual values.
func (f Finding) line() string {
	switch f.Status {
	case StatusMatch:
		return fmt.Sprintf("✓ %s: %s", f.Fact, f.Actual)
	case StatusUnknown:
		return fmt.Sprintf("? %s: undeterminable (expected %s)", f.Fact, f.Expected)
	case StatusUnverifiable:
		return fmt.Sprintf("⚠ %s: %s (expected %s)", f.Fact, f.Actual, f.Expected)
	default: // StatusDrift
		return fmt.Sprintf("✗ %s: expected %s, actual %s", f.Fact, f.Expected, f.Actual)
	}
}

// writeNote appends the marker schema-skew note, when there is one, so a report
// read from an older or newer marker says so ("upgrade smith" for a newer one).
func writeNote(b *strings.Builder, skew marker.Skew) {
	if note := skew.Note(); note != "" {
		fmt.Fprintf(b, "  note: %s\n", note)
	}
}
