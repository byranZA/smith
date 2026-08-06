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
`sshd -T -C user=smith,host=localhost,addr=127.0.0.1` (cheap assert that `pubkeyauthentication`
is `yes`) → loopback-probe `ssh smith@localhost true` → **on any failure, remove the drop-in,
reload sshd, and exit non-zero**. Only if the loopback probe succeeds does hardening stand.
Smith's admin-side `ssh smith@host` is a **post-confirm**, not the gate — by the time Go
reconnects, the box has already proven `smith` can log in under the hardened config and
self-reverted if not.

The loopback authenticates with a **[probe key](../../CONTEXT.md)** — a throwaway keypair minted
on the box for the self-test — not the operator's key. `ssh-keygen` in a `mktemp -d`, append the
public half to `smith`'s real `authorized_keys` (a marked line, so the live handshake exercises
that file's perms/ownership/StrictModes), `ssh -i <probe> -o IdentitiesOnly=yes smith@localhost`,
then scrub both halves and the marked line under an `EXIT INT TERM` trap. The self-test also
strips stale marker lines at its *start*, so a crashed prior run can't leave a live probe line
during a re-run before `smith-keys` reconverges. This makes the gate depend on **nothing** of the
operator's — not their key, agent, forwarding, passphrase, or `ssh_config` (see [issue #47](https://github.com/byranZA/smith/issues/47)).

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
- **Loopback with the operator's key** (via agent forwarding, or the operator's copied key) —
  rejected: the operator's private half never lives on the box, so the on-box loopback can only
  present it by forwarding the agent — which succeeds only on a narrow happy path (right key
  loaded, forwarding on, no passphrase, no bastion). It reintroduces a hidden precondition the
  gate is supposed to be free of. The probe key removes the dependency entirely ([issue #47](https://github.com/byranZA/smith/issues/47)).

## Consequences

- **Reachability invariant:** no base-layer phase but `ssh-hardening` closes a door, and it
  self-reverts on failure, so a mid-sequence failure is *never* a lock-out — worst case is a
  partial-but-fully-reachable box (the substrate the failure-reporting UX builds on, [issue #8](https://github.com/byranZA/smith/issues/8)).
- This is the base-layer half of [lock-out safety](../../CONTEXT.md); the tailscale half — probe
  `ssh` over the tailnet before closing public SSH — lives in [ADR-0001](./0001-tailscale-ssh-keyless-tagged-node.md).
- **The gate proves *a* pubkey login for `smith` survives hardening, not the operator's specific
  key.** Because the drop-in (`PermitRootLogin no`, `PasswordAuthentication no`) can't discriminate
  by key — it touches neither pubkey algorithms nor `smith`'s acceptance path — the probe key is a
  *faithful* proxy: "sshd reads this `authorized_keys` file" holds per-file, so the operator's line
  gets the same fair shot. The one residual — a *byte-corrupted copy* of the operator's line that a
  fresh probe line wouldn't share — is delegated to the admin-side post-confirm. That is a
  deliberate, narrowed re-exposure of the post-close check this ADR otherwise rejects: it's
  accepted because `smith-keys` copies bytes verbatim (`cp`) from a login demonstrably in use, so
  the corruption window is tiny.

See [issue #7](https://github.com/byranZA/smith/issues/7).
