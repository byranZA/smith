> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Staging converge protocol

**Date:** 2026-08-22
**Source session:** review-and-task against spec #91 (`feature/blueprint-on-box`)

## One-line summary

Converge applies the staged tree by concatenating remote shell recipes over SSH; a grill should decide whether that apply protocol should become named on-box operations instead.

## Goal

Immediate: decide whether `staging.Converge` should stop being a laptop-side shell-string DSL.

Broader: keep `internal/staging` a deep module that owns the `/etc/smith/` contract, with an apply step whose failure modes are honest and whose tests describe staged-tree behavior rather than argv.

## Context the next agent needs

Slice 5 (spec #91) stages the operator's blueprint verbatim at `/etc/smith/blueprint.yaml` and resolved placement bytes at `/etc/smith/placements/`. Plan, Resolve, and Load are small surfaces. Converge is the applying seam: it takes a resolved tree plus a narrow `Conn` (`Run`, `RunWithInput`) and writes/prunes over SSH.

Today Converge is five `fmt.Sprintf` recipes in `internal/staging/converge.go` (`digestCommand`, `listCommand`, `pruneCommand`, `dirCommand`, `writeCommand`) plus a sequential loop: ensure dirs, write document then placements, then prune. Digest and list probes swallow failure with `2>/dev/null || true`, so a real `sudo`/`find`/`sha256sum` failure looks like "absent". A mid-loop write failure returns after some files have landed — a half-staged box, which Resolve was written to prevent on the *source* side.

The spec's own testing decision asked for a fake connection recording every remote command and its stdin, in the style of `internal/bootstrap/runner_test.go`. That is how the credential-never-in-argv scenario is verified. The review finding is that those tests then froze the *how* (`sha256sum` / `tee` / `install` / `rm -rf`) as the contract.

Bootstrap already ships `internal/bootstrap/bootstrap.sh` and runs named ops. Staging reinvented a second remote protocol beside it.

A scoped issue (filed from this review) asks only to stop tests sniffing command text. Do not treat that issue as having decided this restructure.

## What was discussed

Quality-axis review of the #91 diff. Plan/Resolve/Load held up as deep modules; Converge did not.

The proposed code-judo: ship named on-box operations (bootstrap.sh subcommands, or one apply script) that take the tree and report staged/updated/pruned; apply the whole tree in one remote transaction; delete the command builders; assert on `Result` and round-trip bytes, not argv.

Counter-pressure from the spec itself: bytes must travel over SSH stdin never argv; each file writes to a temp path inside `0700` then chmod/chown/mv; all writes go through sudo; check-before-change (digest match → unchanged); prune to the declared set; a fake Conn is the specified test seam. Any replacement still has to satisfy those, and must not become a second on-box format (ADR-0006: no derived placement manifest).

## Decisions made

- **Do not auto-issue this as a task** — *firm* — it changes how the apply layer is organized (ownership of the remote protocol, atomicity of the tree). Needs `/grill-me` first.
- **A smaller test-shape fix is separately issued** — *firm* — stop substring-matching `sha256sum`/`find` in fakes. That can land without this restructure.

## Open questions

- **Named ops in `bootstrap.sh`, a new staging script, or stay on laptop-built commands?** — proposal: one apply script owned by `internal/staging` and uploaded/invoked once per Converge, so the shell lives next to the Go that specifies the tree. *(proposal, not committed)*
- **What does "one remote transaction" mean on SSH?** — proposal: one `RunWithInput` that receives the document + placement bytes (length-prefixed or a tar) and reports a result record, rather than N writes plus a prune. Watch atomicity vs. a mid-script crash still leaving partial files. *(proposal, not committed)*
- **How to keep credential-never-in-argv without asserting on `tee`?** — proposal: the apply protocol's contract is "stdin carries bytes, argv carries only paths/modes". Tests feed a secret and assert it is absent from recorded argv. *(proposal, not committed)*
- **Is `2>/dev/null || true` on digest/list a real correctness bug or an accepted absent-file encoding?** — proposal: treat it as a correctness bug if sudo failure is indistinguishable from absent; encode absence explicitly. *(proposal, not committed)*

## Suggested next steps

1. Grill whether Converge should remain a sequence of laptop-built sudo recipes or become one named on-box apply.
2. If named apply: pin the wire format (how bytes, modes, owners, and the prune set cross SSH) so it cannot grow into a second on-box schema.
3. Decide how a mid-apply crash is reported vs. the spec's "staging failure partway through leaves the run non-zero naming the write that failed".
4. Replace `digestCommand`/`writeCommand`/friends; keep `Conn` as the boundary.
5. Retarget tests at `Result` + round-trip bytes; keep the stdin-vs-argv secret assertion.

## Key references

- Spec: https://github.com/byranZA/smith/issues/91
- `internal/staging/converge.go` (especially `Converge`, `digestCommand`, `listCommand`, `writeCommand`)
- `internal/staging/converge_test.go`
- ADR-0006 (verbatim staged document; machine setup sole writer)
- ADR-0002 / ADR-0009 (stdin not argv; plaintext at rest)

## Tried and rejected

- Leaving Converge as-is because behavior is correct — rejected by the quality axis: the shell DSL, silent fallbacks, and half-applied writes are the maintainability problem, not missing features.
- Auto-creating a task to "just extract helpers" — rejected: extracting `fmt.Sprintf` wrappers does not delete the second remote protocol.
