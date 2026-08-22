# Secrets are references, not values — over SSH stdin, never argv

> **Extended by [ADR-0009](./0009-provisioned-secrets-sit-in-plaintext.md).** The `scheme:arg`
> primitive below is now also the Blueprint's value grammar for *provisioned* secrets, where the
> known-scheme set gains `literal:` and an unrecognised scheme is a hard validation error. The
> rule for the operational secret this ADR is about — a bare literal on argv is a hard error — is
> unchanged.

## Context

Bootstrap needs the Tailscale auth key on the box transiently. The tempting shortcut is to accept
the key as a literal on the command line (`--tailscale-auth-key tskey-abc…`) and pass it to the
remote as a script argument.

## Decision

Config carries a **reference**, never the secret itself: one flag `--tailscale-auth-key
<scheme:arg>` where scheme ∈ `env:VAR` | `file:/path`, split on the **first colon only** (so
Windows `file:C:\…` survives). Omitting it prompts securely on the TTY (the default). A bare
literal value on argv is a **hard error** — `env:` is the escape hatch. Values are
whitespace-trimmed. The secret reaches the box over **SSH stdin, never argv**, and is **never
persisted** on the box.

## Consequences

- No secret lands in shell history, `ps` output, process argv, or config files on either side.
- The `scheme:arg` slot is clean-room for future backends (`op:`, `keychain:`, `pass:`) and a
  future provisioning surface — additive, no rewrite.
- `+env` means the `env:` scheme, **not** a `SMITH_*` per-flag fallback.
- Non-interactive + tailscale mode + no reference ⇒ fail fast (nothing to prompt).

See [issue #5](https://github.com/byranZA/smith/issues/5).
