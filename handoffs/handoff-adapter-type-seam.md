> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Adapter type seam

**Date:** 2026-08-22
**Source session:** review-and-task on spec #90 (`feature/provider-adapter-90`)

## One-line summary
`provider.Adapt` is a field-for-field copy from `blueprint.Provider` into a duplicate `Adapter`/`Marker`/`Record`/`Extract` type family, including `Marker.Read` and `Expect`, which v1 never reads.

## Goal
Immediate: decide whether the document/runtime seam has earned its keep, or whether one type should own the adapter until the shapes actually diverge.
Broader: keep yaml tags off the execution path *if* that constraint is real, without carrying unread fields and a copy function whose only tests assert the copy.

## Context the next agent needs
`internal/blueprint` owns what an operator may write. `internal/provider` owns what smith runs. `Adapt` (`internal/provider/adapt.go`) copies every field:

- templates (`Create`, `List`, `Destroy`)
- `Requires`, `SSHKey`
- `Marker.{Arg,Read,Expect}`
- `Record.{Create,List}`
- `Extract.{ID,IP}`

`Create` uses templates, `Requires`, `SSHKey`, `Marker.Arg`, record paths, and extract paths. Spec #90: `read` and `expect` are validated and unused in v1 — "a test asserting otherwise would be asserting a fiction."

`provider.Box` currently has an always-empty `Marker string` that `extractRecord` never sets. Scoped issue #159 drops that field; this hand-off is the larger duplicate-type question, not that field.

The current comment on `Adapt` is the case *for* the seam: document format can change without reaching into the execution path. Quality review's case *against*: the types have not diverged, two tests exist only to assert the copy, and unread marker fields ride along.

Related scoped issues (do not redo them here):

- #159 — drop always-empty `Box.Marker`
- #90 named consequence — `marker.read` / `expect` stay in the *document* schema even with no runtime reader

## What was discussed
Quality review called this a blocker: deleting `Adapt` loses no behaviour. Two remedies were on the table:

1. Keep one type until document and runtime diverge.
2. If yaml tags must stay off the execution path, copy only fields `Create` uses and stop carrying unread marker fields on the runtime type.

Option 2 still keeps a seam; it just stops pretending `Read`/`Expect` are part of what smith runs.

## Decisions made
- **This is grill-first, not an auto-task.** — *firm* — ownership-boundary change.
- **Document schema still accepts `read` and `expect`.** — *firm* — spec #90; adapters written today stay valid when discovery ships.
- **Runtime must not grow a v1 reader for those fields.** — *firm* — same spec named consequence.

## Open questions
- **Is "yaml tags off the execution path" a real constraint, or a hypothetical?** — proposal: if nothing but `Adapt` depends on it, one type is simpler. *(proposal, not committed)*
- **If the seam stays, does runtime `Marker` keep `Read`/`Expect`?** — proposal: no; copy `Arg` only until a reader exists. *(proposal, not committed)*
- **Does `Destroy` belong on the runtime `Adapter` at all in v1?** — it is accepted, never executed. Same "carry unused data" smell as `Read`/`Expect`. *(proposal, not committed)*

## Suggested next steps
1. Run `/grill-me` against this hand-off before writing code.
2. Diff `blueprint.Provider*` against `provider.Adapter`/`Marker`/`Record`/`Extract` field-by-field; confirm they are still copies.
3. List call sites of `Adapt` (CLI `resolveAdapter`, tests) and ask whether they need a distinct type.
4. If collapsing: one type in `provider` (or `blueprint`) with yaml tags, delete `Adapt` and `adapt_test.go`.
5. If keeping the seam: runtime `Marker` holds `Arg` only; `Read`/`Expect` stay on the document type and are validated there.
6. Coordinate with #159 so a Box-field drop and an Adapter-type collapse do not fight.

## Key references
- https://github.com/byranZA/smith/issues/90
- `internal/provider/adapt.go`
- `internal/blueprint/blueprint.go` (`Provider` and nested types)
- `internal/provider/provider.go` (`Adapter`, `Marker`, `Record`, `Extract`, `Box`)
- `docs/adr/0006-the-blueprint-config-surface.md`
- `docs/adr/0007-the-provider-adapter-is-data.md`

## Tried and rejected
- **Start extracting via `Marker.Read` in v1** — rejected by spec #90: no code path reads `read`/`expect`; a test asserting a reader would be asserting a fiction. #159 drops the empty `Box.Marker` instead.
