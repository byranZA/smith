# No local config file in v1 — flags + env only

## Context

A provisioning tool usually grows a local config file (declare your box, your secrets, your env
once; re-run against it). We deliberately did **not** add one for the setup domain in v1.

## Decision

The v1 config surface is **pure CLI flags + env, no local config file**. The line was drawn by a
distinction in the domain: **operational** secrets (consumed then discarded — the Tailscale key,
never persisted) vs **provisioned** secrets/env (installed *onto* the box, many, each needing a
name + destination + source). The setup domain has only *operational* secrets — a single key —
which one flag handles. The provisioning surface, and any config file or provider to declare it,
belongs to a later map.

## Consequences

- The reference primitive (`scheme:arg`, see [ADR-0002](./0002-secrets-are-references-not-values.md))
  extends to a file/provider **additively** when the provisioning map arrives — no rewrite of the
  v1 surface.
- Nothing to parse, validate, or locate on the admin machine; the CLI path stays lean (an
  [`AGENTS.md`](../../AGENTS.md) design boundary).
- Re-runnability comes from the on-box marker, not a local file — any admin machine can re-run.

See [issue #5](https://github.com/byranZA/smith/issues/5).
