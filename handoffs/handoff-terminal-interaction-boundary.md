> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Terminal interaction boundary

**Date:** 2026-08-23
**Source session:** `/review-and-task` on issue #130 and branch `feature/on-box-smith-130`

## One-line summary

General terminal interaction has expanded inside `internal/secret`, and the package boundary should be reconsidered before more prompts deepen that ownership mistake.

## Goal

Keep secret acquisition focused on secrets while giving the CLI one coherent, testable boundary for terminal attendance, echoed line input, and hidden input.

## Context the next agent needs

Issue #130 added a version-skew prompt. The implementation reused `secret.StdTerminal`, adding `Attended` and `ReadLine` beside the existing secret-specific `Interactive` and `ReadSecret` methods in `internal/secret/acquire.go`. `internal/cli/skew.go` now depends on a narrow local interface satisfied by that secret package type.

The reuse avoids duplicate `isatty` calls, but it makes unrelated interactive CLI behavior depend on a package whose domain is secret references and secret acquisition. Another prompt would likely extend the same package again.

The repository favors packages by domain concept, deep modules, narrow accepted interfaces, and explicit dependency injection. Any redesign must preserve the distinct attendance rules: secret input only requires terminal stdin, while the version-skew prompt requires both stdin and stdout to be terminals.

## What was discussed

The Quality review treated this as structural rather than a local rename. Moving only the new methods or introducing another CLI-local terminal check would leave duplicated operating-system boundaries. The promising direction is a small terminal-owned module that encapsulates standard streams and terminal detection while consumers retain narrow, behavior-specific interfaces.

## Decisions made

- **Do not auto-create an implementation issue from this review** — *firm* — rationale: changing package ownership is an ambitious restructuring that needs a design discussion first.
- **Preserve different attendance policies for secrets and confirmation prompts** — *firm* — rationale: redirected stdout must not prevent a secret prompt, while it must prevent a mutating confirmation prompt.
- **A dedicated terminal package is the leading option** — *working assumption* — rationale: it restores domain ownership and keeps the operating-system boundary singular, but its public surface still needs grilling.

## Open questions

- **What is the smallest deep interface?** — proposal: expose a concrete standard terminal with `IsInputTerminal`, `IsOutputTerminal`, `ReadLine`, and `ReadSecret`, while `secret` and `cli` accept narrower local interfaces. *(proposal, not committed)*
- **Should prompt rendering belong to the terminal module or the caller?** — proposal: keep wording and policy in callers; let the terminal module own only stream mechanics. *(proposal, not committed)*
- **Should the package be `terminal`, `prompt`, or another domain term?** — no name was selected. *(open)*

## Suggested next steps

1. Run `/grill-me` against this handoff and test whether a separate package is warranted or whether an existing boundary already owns terminal mechanics.
2. Inventory every use of `secret.Terminal`, `secret.StdTerminal`, `Interactive`, `Attended`, `ReadSecret`, and `ReadLine`.
3. Define the attendance policies independently from the stream implementation and verify the four stdin/stdout combinations remain covered.
4. Prototype the narrowest package surface and compare call sites in `internal/secret/acquire.go`, `internal/cli/cli.go`, and `internal/cli/skew.go`.
5. If the boundary holds up, write a focused implementation spec or task with behavior-preserving tests before moving code.

## Key references

- GitHub issue #130: `Spec: on-box smith — install, relay, and version skew`
- `internal/secret/acquire.go`
- `internal/cli/skew.go`
- `.claude/skills/coding-standards/SKILL.md`
- `.claude/skills/coding-standards/GO.md`

## Tried and rejected

- **Leave general interaction in `internal/secret` because it already owns `isatty`** — rejected because sharing an operating-system mechanism does not make confirmation prompts part of the secrets domain.
- **Add a second terminal implementation in `internal/cli`** — rejected because it duplicates the boundary and its test machinery instead of clarifying ownership.
