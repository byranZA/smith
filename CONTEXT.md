# smith

The domain language of smith — turning a fresh VPS into a provisioned, secured, reachable
remote dev machine, then running coding agents on it. This file is the glossary only;
the *why* and the boundaries live in [`AGENTS.md`](./AGENTS.md), architectural decisions in
[`docs/adr/`](./docs/adr/).

## Language

### The machine and getting in

**Box**:
The fresh Ubuntu LTS VPS the user brings. Smith provisions it but does not own or manage its
lifecycle — it *is* the sandbox, not a container smith runs.
_Avoid_: server, host (except in `<login>@<host>`), instance, VM.

**Bootstrap-in path**:
The one-time public-SSH entry smith uses to reach a brand-new box — `smith machine setup
<login>@<host>` over system `ssh`/`scp`, authenticating with the key the provider already
injected. Distinct from the ongoing `ssh smith@host` reach the setup establishes.
_Avoid_: onboarding, first connection.

**Bootstrap login**:
The root-or-sudo-capable account the operator connects *as* during the bootstrap-in path.
Under a non-root login smith runs via `sudo` and fails early if passwordless sudo is absent.
_Avoid_: admin user, initial user.

**smith user**:
The provisioned account smith creates on the box and the operator uses thereafter
(`ssh smith@host`). Gets passwordless sudo — a deliberate blast-radius tradeoff for a
self-managing dev box.

### What "done" means

**Provisioned / Secured / Reachable**:
The three-part destination state of a bootstrapped box. *Provisioned* = smith user, dirs, and
base packages present. *Secured* = hardened SSH, `ufw` default-deny, `fail2ban`, unattended
security upgrades. *Reachable* = the operator can `ssh smith@host` (public or over the tailnet).

**Base layer**:
The always-applied slice of bootstrap — smith user + hardening — identical regardless of how
the box is reached.
_Avoid_: base hardening (as a distinct thing), core setup.

**Access layer**:
The one pluggable choice layered over the base: how the box is reached. `--access=public`
(default) leaves hardened SSH open on the public IP; `--access=tailscale` joins a tailnet and
closes public SSH. Architected as a swappable layer so neither mode re-architects the other.
_Avoid_: connectivity mode, networking option.

**Lock-out safety**:
The invariant that smith never severs its own reach to a box before verifying the new door
opens — "never close the door blind." Applies to both access modes: in tailscale mode, probe
`ssh` over the tailnet *before* closing public SSH; any failure leaves the old door open and
reports.
_Avoid_: rollback, failsafe.

### State on the box

**Phase**:
One named, ordered, mutating step of the base-layer bootstrap sequence — `packages`,
`smith-user`, `smith-keys`, `firewall`, `ssh-hardening`, `fail2ban`, `auto-updates`, `access`
(preceded by a non-recorded `preflight` gate). The names *are* the marker's `completed_phases`
values, so they are load-bearing. A phase appends itself to the marker only on success; the
reachability-affecting phases (`firewall`, `ssh-hardening`, `access`) carry the lock-out gates,
and only `ssh-hardening` closes a base-layer door — which it self-reverts on any local failure,
so a mid-sequence failure is never a lock-out.
_Avoid_: step, stage, task.

**Marker**:
The versioned on-box record of what bootstrap did — `/etc/smith/bootstrap.json` (schema + smith
version + access mode + completed phases + timestamp). Lets `smith machine status` read state
back and any admin machine re-run idempotently. A **ledger, not a gate**: every `setup` run
executes all phases and check-before-change makes done ones no-ops — `completed_phases` records
progress (for failure reports and `status`), it never *skips* execution.
_Avoid_: state file, lockfile, manifest.

**Check-before-change**:
The rule that every bootstrap step verifies the box's current configured state before mutating
it, so re-runs converge instead of duplicating. Governs *configured state*, not the package
manager (which is simply `ensure`d, not probed).
_Avoid_: idempotency (as the term of art — check-before-change is *how* smith achieves it).

**Drift**:
Divergence between what the marker records and what smith re-probes live on the box.
`smith machine setup` mutates to converge; `smith machine status` only *reports* drift and never
remediates it — the read-only, state-oriented half of the setup/status seam.
_Avoid_: skew (reserved for marker *schema* version mismatch), diff.

### Secrets

**Reference, not value**:
The rule that config points *at* a secret (a `scheme:arg` like `env:VAR` or `file:/path`),
never embeds the secret itself. The durable primitive the secret surface is built on.
_Avoid_: secret string, credential literal.

**Operational secret**:
A secret consumed during bootstrap and then discarded — never persisted on the box. The
Tailscale auth key is the only one in the setup domain; it travels over SSH stdin, never argv.
_Avoid_: transient secret, ephemeral credential.

**Provisioned secret**:
A secret *installed onto* the box for later use (e.g. the worker's env). Distinct lifecycle and
surface from operational secrets; out of scope for the setup domain, deferred to a later map.
_Avoid_: stored secret, deployed credential.

### Tailscale access

**tag:smith**:
The ownership tag every smith-enrolled node carries. Kept purely so the node's key never
expires; requires two one-time-per-tailnet user prerequisites (`tagOwners` and an `ssh` ACL
rule) that smith's auth key cannot self-serve.

**Tailscale SSH**:
Keyless SSH over the tailnet — Tailscale is the identity authority, replacing `authorized_keys`
for tailnet connections without touching `sshd`. The v1 tailscale-mode reach.
_Avoid_: tailnet SSH, VPN SSH.

### OS support

**Floor + capability gate**:
How smith decides a box is supported: an LTS *floor* (`ID=ubuntu`, an `LTS` release,
`VERSION_ID` ≥ 24.04, no upper bound) plus *capability probes* for what smith actually needs
(apt, systemd, packages) — never a hardcoded enumeration of releases. Future LTS releases pass
automatically; an unmet need fails loudly at the step, not pre-emptively at the gate.
_Avoid_: version allowlist, supported-OS list.
