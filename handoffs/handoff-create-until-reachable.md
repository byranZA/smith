> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Create until reachable

**Date:** 2026-08-22
**Source session:** review-and-task on spec #90 (`feature/provider-adapter-90`)

## One-line summary
`machine create` talks to the provider, waits for an address, then waits for port 22 — but that sequence is split across three packages with cloned `Clock`/`Runner` seams and two identical poll loops.

## Goal
Immediate: decide whether "the box exists and is reachable" should be one function with a small interface, so cobra only loads config and prints.
Broader: keep the provider adapter as data (ADR-0007) without leaking orchestration into `internal/cli` or bolting TCP dials onto `internal/connection`'s ssh/scp job.

## Context the next agent needs
Spec #90 / ADR-0007: create runs the adapter, extracts `{id, ip|null}`, polls `list` when the address is null, then polls port 22. v1 does not destroy; failure messages must name the box id (and address if known).

Current split on `feature/provider-adapter-90`:

- `provider.Create` hides render / run / extract and the address poll (`internal/provider/poll.go` `awaitAddress`).
- CLI then calls `awaitReachable` → `connection.WaitForSSH` (`internal/cli/machine.go` ~53–96).
- Both polls are deadline / attempt / `ctx` vs `Clock.After`.
- `provider.Clock` and `connection.Clock` are the same interface, copied.
- `provider.Runner` and `connection.Exec` are the same "run a process" seam — `cli.go` passes `connection.System()` as `provider.Runner`.
- `fakeClock` exists in provider, connection, and cli tests.
- `WaitForSSH` lives in a package whose documented job is ssh/scp exec, and owns TCP dials because the second poll needed a home.

Do **not** fold TCP wait into `provider` — that mixes CLI-talk with sockets. The proposed judo is a single create-until-reachable function *above* both, plus one process-runner seam and one poll-until (one `Clock`).

Related scoped issues already filed under #90 (do not redo them in this grill):

- #157 — address poll extracts every list row before matching id
- #158 — rename `internal/jsonpath` away from JSONPath
- #159 — drop always-empty `Box.Marker`
- #160 — nest the extractor grammar under `provider` (blocked by #158)

## What was discussed
Quality review treated this as a blocker: `Create` is a deep module for "talk to the provider CLI"; everything after that is a pile. ADR-0007 describes one sequence (list-by-id, then port 22). The clones (`Clock`, `Runner`/`Exec`, two retry loops) are the complexity to delete, not rearrange.

A competing lean: keep `WaitForSSH` in `connection` because a brought-your-own box becomes reachable the same way — the poll is not provider-specific. That argument is in the `WaitForSSH` comment today. The grill needs to settle whether "reachable" is create-pipeline-only in v1 or a shared primitive.

## Decisions made
- **Do not fold TCP wait into `provider`.** — *working assumption* — from the quality finding; preserves the CLI-talk vs TCP boundary.
- **This is grill-first, not an auto-task.** — *firm* — review-and-task routes ambitious restructures to a hand-off, not a `ready-for-agent` issue.

## Open questions
- **Where does create-until-reachable live?** — proposal: a small function in `provider` that accepts a dialer, *or* a new thin orchestrator the CLI calls once. *(proposal, not committed)*
- **Is `WaitForSSH` a shared primitive for bring-your-own-box, or create-only in v1?** — spec #90 only covers create. `WaitForSSH`'s comment claims the operator-brought path too. *(proposal, not committed)*
- **One `Clock` and one process runner in which package?** — proposal: one runner at the process boundary (`connection.Exec` or a shared `internal` type); one `Clock` next to the single poll-until. *(proposal, not committed)*

## Suggested next steps
1. Run `/grill-me` against this hand-off before writing code.
2. Read `internal/cli/machine.go` `newCreateCmd` / `awaitReachable`, `internal/provider/poll.go` `awaitAddress`, `internal/connection/readiness.go` `WaitForSSH`, and the `Clock`/`Runner`/`Exec` definitions.
3. Sketch one function whose tests cover: create with address, create then list-poll, port-22 refuse-then-accept, timeout naming id+address. Keep cobra as load-config / call-once / print.
4. Decide whether `WaitForSSH` stays public for a future bring-your-own path or becomes an unexported detail of that function.
5. Do not start this work while #157 is open — the address-poll match-by-id bug is in the same loop you would delete.

## Key references
- https://github.com/byranZA/smith/issues/90
- `docs/adr/0007-the-provider-adapter-is-data.md`
- `internal/provider/poll.go`
- `internal/connection/readiness.go`
- `internal/cli/machine.go`
