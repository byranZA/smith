# Base-layer lock-out gate is an on-box self-reverting self-test

## Context

The `ssh-hardening` phase is the only base-layer step that closes a door: it applies
`PermitRootLogin no` and `PasswordAuthentication no`. If it lands a config that locks the
operator out (bad drop-in, a key that doesn't actually work for `smith`), the box is bricked —
no way back in. The obvious safety nets are to keep root logins enabled as a surviving backstop,
or to soften to `PermitRootLogin prohibit-password`, or to rely on smith's admin-side
`ssh smith@host` reconnect as the go/no-go gate.

## Decision

The gate is an **on-box, self-reverting self-test performed inside the `ssh-hardening` phase**,
before the phase reports success: write the drop-in → `sshd -t` (validate) → reload sshd →
loopback-probe `ssh smith@localhost true` → **on any failure, remove the drop-in, reload sshd,
and exit non-zero**. Only if the loopback probe succeeds does hardening stand. Smith's admin-side
`ssh smith@host` is a **post-confirm**, not the gate — by the time Go reconnects, the box has
already proven `smith` can log in under the hardened config and self-reverted if not.

## Considered options

- **Surviving-root backstop** (keep root SSH enabled as a fallback) — rejected: it permanently
  weakens the hardening the phase exists to apply, trading the destination's *secured* property
  for a safety net the self-revert provides without that cost.
- **Soften to `PermitRootLogin prohibit-password`** — rejected: same reason, it isn't full
  hardening. The self-revert lets us honor `PermitRootLogin no` outright.
- **Admin-side reconnect as the gate** — rejected as *the* gate: it's a remote check that only
  fires *after* the door is closed, so a failure is already a lock-out. The on-box self-revert
  makes a blind close structurally impossible; the admin-side reconnect stays, demoted to a
  post-confirm.

## Consequences

- **Reachability invariant:** no base-layer phase but `ssh-hardening` closes a door, and it
  self-reverts on failure, so a mid-sequence failure is *never* a lock-out — worst case is a
  partial-but-fully-reachable box (the substrate the failure-reporting UX builds on, [issue #8](https://github.com/byranZA/smith/issues/8)).
- This is the base-layer half of [lock-out safety](../../CONTEXT.md); the tailscale half — probe
  `ssh` over the tailnet before closing public SSH — lives in [ADR-0001](./0001-tailscale-ssh-keyless-tagged-node.md).

See [issue #7](https://github.com/byranZA/smith/issues/7).
