> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Workspace Phase Dependencies

**Date:** 2026-08-23
**Source session:** review-and-task of issue #123 on `feature/workspace-stage-123`

## One-line summary

The workspace executor needs an explicit failure-dependency model so independent repos continue while phases do not run after their prerequisites fail.

## Goal

Preserve issue #123's per-unit failure collection without launching dependent work through missing credentials, packages, or toolchain state.

## Context the next agent needs

`workspace.Plan` currently returns a flat `[]Unit` ordered as placements, packages, toolchain, repos, and orphans. `workspace.Converge` loops over every unit after every failure. The spec says the order is load-bearing: placements may supply clone credentials, and toolchain config supplies environment values used by `mise exec`. It also says one unreachable repo must not block sibling repos and every failure must be reported.

## What was discussed

The flat tagged-unit model expresses ordering but not dependency. Continuing after a repo failure is correct because repos are siblings. Continuing into repo clones after a credential placement or toolchain failure can create cascading, misleading errors and can act through stale config from a prior run. A better model should distinguish phase prerequisites from independent units within a phase and should represent skipped work in the report.

## Decisions made

- **Treat this as an ambitious structural finding, not a local conditional** — *working assumption* — rationale: scattered `if prior step failed` checks would entrench the missing model instead of making dependencies explicit.
- **Keep sibling repo continuation and aggregate reporting** — *firm* — rationale: both are explicit acceptance criteria in issue #123.

## Open questions

- **Which phases block which later phases?** — proposal: placement/package/toolchain failures should block repos only when the failed unit is a real prerequisite; decide whether packages are globally blocking or independent. *(proposal, not committed)*
- **How should skipped outcomes render and affect exit status?** — proposal: report them distinctly from failures while retaining a non-zero overall result. *(proposal, not committed)*
- **Should orphan scanning still run after earlier failures?** — proposal: yes, because it is read-only and does not depend on successful convergence. *(proposal, not committed)*

## Suggested next steps

1. Run `/grill-me` against issue #123's ordering and per-unit-failure requirements.
2. Model phases and dependency edges in `internal/workspace/plan.go` without changing observable success behavior.
3. Add acceptance tests for placement failure, toolchain failure, sibling repo failure, and orphan reporting after failure.
4. Update `internal/workspace/converge.go` and `internal/workspace/report.go` only after the state model is agreed.
5. Re-run `make check`, `go build ./...`, and the workspace package tests.

## Key references

- https://github.com/byranZA/smith/issues/123
- `internal/workspace/plan.go`
- `internal/workspace/converge.go`
- `internal/workspace/report.go`
- `/Users/byran/.codex/skills/claude-skills/review-and-task/QUALITY-LENSES.md`
