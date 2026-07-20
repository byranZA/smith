# Handoff: derive phase-failure state from the marker instead of parsing progress glyphs

Note to the receiving agent: This is a hand-off doc from a `/review-and-task` session on 2026-07-20. It captures an *ambitious* restructuring surfaced by the Quality review of the `feat/vps-setup` branch — deliberately **not** auto-issued, because it changes a bash↔Go contract and deserves a `/grill-me` or `/brainstorm` pass before it becomes work. Read the Open questions before writing code.

## Goal

Stop reconstructing which setup phase failed by re-parsing human-facing progress output, and instead derive it authoritatively from the marker the box already writes. Delete the glyph-prefix wire contract and the stdout tee, keeping the `▶`/`✓` glyphs purely for the operator.

## Context

On a setup failure, `Setup` tees `bootstrap.sh` stdout into a buffer and `parseSetupStream` (`internal/bootstrap/report.go:60-80`) re-parses the `▶ ` / `✓ ` emoji-prefixed progress lines — plus the ` (already-satisfied)` suffix — to reconstruct which phases completed and which one failed. See `internal/bootstrap/runner.go:182-203` and the `phaseStartPrefix` / `phaseDonePrefix` / `satisfiedSuffix` constants in `report.go`.

This makes user-facing progress text a **machine wire-format**: any change to the glyphs or wording in `bootstrap.sh` silently breaks failure parsing, with no compile-time or test signal tying the two together.

But the state is already available authoritatively:
- `bootstrap.sh` writes `completed_phases` to the marker after every phase via `mark_complete`.
- `internal/status/reconcile.go:29-32` (`expectedPhases`) already encodes the canonical ordered phase set (see also issue #22, which dedupes that list).

So on failure: `completed` = the marker's `completed_phases`, and `failed = expectedPhases[len(completed)]` (the first expected phase not yet completed). `RawError` still comes from the stderr buffer.

## The proposed restructuring

1. On a non-connect `Setup` failure, read the marker from the box (reuse the existing probe / marker-read path) to get `completed_phases`.
2. Derive `failed` = first `expectedPhases` entry not in `completed`.
3. Keep `RawError` from the stderr buffer.
4. Delete `parseSetupStream`, the stdout tee buffer in `runner.go`, and the `phaseStartPrefix` / `phaseDonePrefix` / `satisfiedSuffix` contract.
5. Leave the `▶`/`✓` glyphs in `bootstrap.sh` untouched — they remain operator-facing only, with nothing in Go depending on their exact bytes.

## Open questions

- **Marker readability after a mid-phase crash.** If a phase dies partway, is the marker guaranteed to reflect the last *completed* phase and never a half-written state? Confirm `mark_complete` is atomic (write-temp-then-rename) and only ever runs at a phase boundary. If a phase can fail *after* marking itself complete, `expectedPhases[len(completed)]` names the wrong phase.
- **Connect-time failures.** When the box is unreachable there is no marker to read. The current path presumably already special-cases connect failures — confirm the derive-from-marker path is only taken for on-box (non-connect) failures.
- **Extra round-trip.** Reading the marker on failure adds an SSH round-trip on an already-failing box. Acceptable? Or should `bootstrap.sh` emit the completed set on a dedicated machine-readable fd (a genuine wire-format) so no second connection is needed?
- **already-satisfied vs completed.** `parseSetupStream` currently distinguishes "ran" from "already-satisfied" via the suffix. Does the failure report need that distinction, or is completed-vs-not sufficient? If it's needed, the marker must record it too.
- **Interaction with issue #22.** That issue dedupes the triplicated phase list. Decide ordering: dedupe first (single source of truth for `expectedPhases`) makes this refactor cleaner.

## Suggested next steps

1. `/grill-me` on the Open questions above — especially marker atomicity and the connect-failure boundary.
2. Confirm `mark_complete` semantics in `bootstrap.sh` (atomic write, phase-boundary only).
3. Decide: read-marker-on-failure vs. a dedicated machine-readable output channel from `bootstrap.sh`.
4. Sequence after issue #22 (phase-list dedupe) so `expectedPhases` is single-sourced.
5. Implement, then delete `parseSetupStream` + the glyph-prefix constants + the stdout tee, and add a test that a mid-run failure reports the correct failed phase without depending on any glyph bytes.
