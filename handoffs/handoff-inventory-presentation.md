> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Inventory presentation vs identity

**Date:** 2026-08-22
**Source session:** /review-and-task on `feature/box-inventory-92` against [issue #92](https://github.com/byranZA/smith/issues/92)

## One-line summary
The box-inventory package owns identity (name → target) cleanly, but this slice also put CLI listing, connect hints, and the `smith` login inside it — a boundary that needs a grill before anyone moves the code.

## Goal
Decide whether `internal/inventory` stays an identity-only address book, with listing and operator-facing hints living in `internal/cli`, or whether presentation is a legitimate second responsibility of the package. The immediate goal of this grill is that ownership boundary; the broader objective is keeping ADR-0011's "identity only" claim true of the *package*, not just the file on disk.

## Context the next agent needs
Issue #92 shipped `~/.smith/cache/boxes.json` as a name → proven-target address book. ADR-0011 is the decision: two fields of identity, no access mode, no `last_seen`, no second copy of marker facts. Reach is displayed by `machine list --probe` and never stored.

The identity operations in `internal/inventory` match that: `Read`/`Write`/`Register`/`Rename`/`Forget`/`Resolve`/`Name`. Three things this slice added sit beside them and do not:

- `Readout` (`internal/inventory/readout.go`) takes a `reach map[string]bool` — a value the ADR says is never stored — and renders the `NAME`/`TARGET`/`REACHABLE` table, including the `smith machine add` hint on the empty listing.
- `ConnectHint` (`internal/inventory/resolve.go`) embeds the recovery command `smith machine add smith@<arg>` and a `smith` login constant (`smithUser`).
- `internal/cli/machine_setup.go` has a second copy of that login as `smithLogin`, used to build ongoing targets (`smith@<host>`).

The quality review on this branch treated this as a **code-judo** move, not a nit: display data and CLI verb names do not belong in an identity-only address book. It was *not* auto-issued — it changes package ownership, so it needs `/grill-me` (or `/brainstorm`) before it becomes work.

Related scoped issues already filed under #92 (do not redo them here): the `@` skip-read living in both CLI and inventory; `status.Reachable` sitting in a layer that cannot be canonical; finishing the setup extraction out of `machine.go`.

## What was discussed
The quality axis applied the lenses from the review-and-task skill. The inventory identity core scored well: small files, copy-on-write mutations, one collision rule. The cost was in how surrounding layers absorbed it.

The presentation leak is the ambitious finding. Two plausible frames:

1. **Inventory is the map.** Listing is a CLI formatter over that map. Hints are CLI copy. The `smith` login belongs in `cli` (one helper building ongoing targets *and* hints). Inventory then has no reach map and no command strings. This is the review's recommended frame, and it is the one that makes ADR-0011's "the inventory stores identity and nothing else" true of the Go package as well as `boxes.json`.

2. **Inventory owns the operator-facing readout of itself.** Session list already lives next to session state (`#76` settled one list style). Putting `Readout` next to `Decode`/`Encode` keeps the listing contract in one place and stops `cli` from re-deriving column rules. `ConnectHint` is the cost of "the `@` decides", so it arguably belongs with `Resolve`. The `smith` login duplication is then a small extract, not a package split.

The review preferred (1) because a reach map and a `smith machine add` string are not identity. The counter is that `Readout` is currently a pure function of `(Inventory, Skew, reach)` — moving it to `cli` does not delete complexity, it relocates a formatter. That is why this is a grill, not a task.

## Decisions made
- **Do not auto-issue this as a `ready-for-agent` task** — *firm* — rationale: the review-and-task skill sends ambitious refactors to a hand-off for a grill session; the ownership boundary is not a clear-path extract.
- **Identity-only remains the file-shape rule** — *firm* — rationale: ADR-0011; this grill is about the *package*, not about adding fields to `boxes.json`.
- **Reach is never stored** — *firm* — rationale: ADR-0011; wherever `Readout` lives, the reach map stays a display input.

## Open questions
- **Does ADR-0011 constrain the Go package, or only `boxes.json`?** — proposal: constrain the package too, so a contributor's instinct to cache access mode in inventory (the pressure the ADR exists to hold) has nowhere to land. *(proposal, not committed)*
- **Is `Readout` closer to session list (domain package renders its own listing) or to CLI formatting?** — proposal: CLI, because the reach column is a `machine list --probe` concern and the empty-state hint names a CLI verb. *(proposal, not committed)*
- **Does `ConnectHint` move with `Readout`, or stay next to `Resolve`?** — proposal: move both; a hint that names `smith machine add` is CLI copy. A domain-shaped alternative is returning structured facts (`unrecognised bare value`) and letting CLI render. *(proposal, not committed)*
- **One `smith` login helper: `cli` or `connection`?** — proposal: `cli`, because building `smith@host` is how setup names the ongoing target, not how `ssh` connects. *(proposal, not committed)*

## Suggested next steps
1. Run `/grill-me` (or `/grill-with-docs`) on this hand-off with ADR-0011 and `docs/inventory.md` in scope.
2. Ask the user to pick a frame: inventory-is-the-map vs inventory-owns-its-readout.
3. If frame 1: sketch the move — `Readout` + `ConnectHint` to `cli`, one `smithLogin` helper in `cli`, inventory left with Read/Write/Register/Rename/Forget/Resolve/Name. Confirm tests still assert operator-visible listing text.
4. If frame 2: extract only the duplicated `smith` login, leave `Readout`/`ConnectHint`, and write a sentence in ADR-0011 that presentation of the address book is in-package.
5. Do not combine this with the scoped `@`-rule or `Reachable`-layer issues unless the grill concludes they are the same seam.

## Key references
- [Spec: the box inventory](https://github.com/byranZA/smith/issues/92)
- `docs/adr/0011-the-box-inventory-is-identity-only.md`
- `docs/inventory.md`
- `internal/inventory/readout.go`
- `internal/inventory/resolve.go` (`ConnectHint`, `smithUser`)
- `internal/cli/machine_setup.go` (`smithLogin`)
- Session list style from issue #76 (the listing this readout was written to mirror)

## Tried and rejected
- Auto-creating a `ready-for-agent` task to "move Readout to cli" — rejected because the ownership question is not settled and a mechanical move can make the listing worse (CLI re-deriving column rules, tests asserting on the wrong layer) without a design pass.
