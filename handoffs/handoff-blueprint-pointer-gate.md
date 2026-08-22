> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Blueprint pointer gate

**Date:** 2026-08-22
**Source session:** review-and-task against spec #91 (`feature/blueprint-on-box`)

## One-line summary

The blueprint-pointer refusal (no `--blueprint` against a box whose marker records one) lives in `internal/staging` even though it is a marker/setup-gate invariant, not staged-tree logic.

## Goal

Immediate: decide which package owns the pointer rule and how the marker is read.

Broader: keep `internal/staging` as the `/etc/smith/` *config contract* (Plan / Resolve / Converge / Load) and keep setup-gate refusals with the rest of `machine setup`'s gates (preflight, connect, exit 2).

## Context the next agent needs

Spec #91: "No blueprint supplied, but the marker records one → refuse, naming the blueprint. This is the only rule under which the marker's blueprint pointer cannot come to describe a box that no longer matches it."

Implemented as `staging.CheckPointer` in `internal/staging/pointer.go`, wired from `checkBlueprintPointer` in `internal/cli/machine.go` after preflight and before `runner.Setup`. It `cat`s `/etc/smith/bootstrap.json` *without* sudo (unlike every Converge command), `marker.Decode`s, and returns `PointerError`. Staging now also exports `MarkerPath`. Tests introduce a third Conn fake (`pointerBox`) beside Converge's `fakeBox` and the CLI's `fakeStagingBox`.

Status already probes and decodes the same marker via bootstrap. The pointer is a marker fact; GO.md wants one package per domain concept.

A scoped HITL issue from this review (#164) covers only the small seam (move `MarkerPath` into `marker`, or document why staging keeps it). This hand-off is the larger move: delete `pointer.go`, read through the existing probe, refuse in the setup gate, CLI prints and maps exit 2, staging never mentions `bootstrap.json`.

## What was discussed

Quality axis: `CheckPointer` does not Plan, Resolve, Converge, or Load. It is a setup gate in the wrong package.

Standards axis (judgement): the rule protects staged-tree honesty, so living next to the tree is arguable — but the pointer is still marker data.

Spec coverage of the *behavior* is fine; this is ownership.

## Decisions made

- **Do not auto-issue the deletion of `pointer.go`** — *firm* — it changes package boundaries and how the setup gate reads the box. Needs `/grill-me`.
- **The pointer rule's behavior stays** — *working assumption* — refuse, name the blueprint, mutate nothing, exit 2. The grill is about *where* it lives, not whether it exists.
- **#164 is the small seam only** — *firm* — do not treat landing #164 as having resolved this.

## Open questions

- **Read the marker through bootstrap's existing probe, a `marker` helper, or keep a tiny staging consumer?** — proposal: one `ReadMarker` (or reuse the status probe) in `marker`/`bootstrap`; CLI/setup gate calls it. *(proposal, not committed)*
- **Should the cat use sudo, matching Converge?** — proposal: yes if `/etc/smith/bootstrap.json` is ever unreadable by the connecting user; today bootstrap writes it root-owned and the bootstrap-in path is root/sudo. Verify against how `machine status` already reads it. *(proposal, not committed)*
- **Does deleting `MarkerPath` from staging conflict with Load's on-box paths?** — Load reads `blueprint.yaml` and placements, not the marker. Staging can forget `bootstrap.json` entirely. *(proposal, not committed)*

## Suggested next steps

1. Grill whether the pointer rule is a staging invariant or a setup-gate invariant (the review's lean is setup-gate).
2. Trace how `machine status` already reads the marker; reuse that path rather than adding a fourth.
3. If moving: delete `pointer.go` and `MarkerPath`; keep `PointerError` next to the gate that prints it (CLI or bootstrap outcome).
4. Collapse the pointer Conn fake into whatever fake already serves status/preflight.
5. Confirm #164 is closed or superseded so two agents do not move the same constant twice.

## Key references

- Spec: https://github.com/byranZA/smith/issues/91 (scenario: re-running setup without a blueprint on a box that has one)
- Related scoped issue: https://github.com/byranZA/smith/issues/164
- `internal/staging/pointer.go`
- `internal/cli/machine.go` (`checkBlueprintPointer`)
- `internal/marker/marker.go`
- ADR-0006 (marker records the blueprint name only)

## Tried and rejected

- Treating "move `MarkerPath`" as the whole fix — rejected as insufficient: the gate, the unsudoed cat, and the extra Conn fake stay.
- Leaving it in staging because the rule protects the staged tree — rejected as the quality-axis default: a rule *about* the marker still belongs with the marker/setup pipeline.
