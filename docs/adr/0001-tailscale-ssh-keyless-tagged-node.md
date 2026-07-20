# Tailscale access is keyless Tailscale SSH on a tagged `tag:smith` node

## Context

`--access=tailscale` needs a reach model over the tailnet. The obvious path is network-only —
join the tailnet, then keep plain OpenSSH with `authorized_keys` over the 100.x IP. We chose the
less obvious one: **Tailscale SSH** (keyless), on a node smith enrolls with a **pre-authorized,
single-use, non-ephemeral, `tag:smith`** auth key via `tailscale up --ssh
--advertise-tags=tag:smith --hostname=smith-<host>`.

## Decision

Tailscale is the identity authority for tailnet reach: no `authorized_keys` on tailnet
connections, revocation in one place (the tailnet policy), and no per-node key management. The
`tag:smith` tag exists **only** so the node's key never expires — not for network segmentation.

## Considered options

- **Network-only (plain OpenSSH over the tailnet)** — rejected for v1. It keeps a second key to
  manage and drops the single-place-to-revoke property. Its one advantage (no second ACL
  prerequisite) wasn't worth re-introducing key sprawl. Returns only if a user asks, as a fresh
  effort. See [issue #4](https://github.com/byranZA/smith/issues/4).

## Consequences

- Tagging pulls in **two one-time-per-tailnet user prerequisites** the auth key cannot self-serve,
  and they fail at *different* stages: `tagOwners` for `tag:smith` (absent ⇒ enrollment never
  reaches `Running`) and an `ssh` ACL rule `dst:tag:smith` using `accept` not `check` (absent ⇒
  node enrolls and gets a 100.x IP but the SSH probe is denied). Smith prints both up front,
  personalized, and detects them reactively.
- The real readiness gate is therefore a **live SSH-over-tailnet probe**, not the `Online`/`Running`
  flag — and that probe is what gates closing public SSH ([lock-out safety](../../CONTEXT.md)).
- Smith never edits the tailnet policy; it stays the user's to own.

Research: [`docs/research/tailscale-ssh-enrollment.md`](../research/tailscale-ssh-enrollment.md).
