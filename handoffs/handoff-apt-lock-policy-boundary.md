> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Apt Lock Policy Boundary

**Date:** 2026-08-23
**Source session:** review-and-task of issue #123 on `feature/workspace-stage-123`

## One-line summary

Go currently reverse-parses embedded Bash source to reuse the apt lock policy, creating a brittle cross-language ownership boundary.

## Goal

Give the bootstrap script and Go workspace stage one robust apt lock policy without maintaining a partial Bash parser or allowing the two consumers to drift.

## Context the next agent needs

Issue #123 requires the workspace package installation to reuse the base layer's first-boot apt lock wait. `internal/bootstrap/apt.go` currently extracts `APT_LOCK_TIMEOUT` and `APT_LOCK_OPTS` from the embedded `bootstrap.sh` with regular expressions, `strings.Fields`, quote trimming, and a package-initialization panic. The current spelling passes tests, but harmless Bash formatting or quoting changes can alter or break the Go arguments.

## What was discussed

The implementation avoids a duplicated timeout constant, but makes Bash source text an implicit configuration API and implements only enough shell parsing for today's line. A structural solution should establish a small canonical policy and derive both consumer representations from it, rather than making either runtime parse the other's language.

## Decisions made

- **Treat this as an ambitious ownership-boundary refactor** — *working assumption* — rationale: simply widening the regular expressions moves fragility around and does not remove the cross-language magic.
- **Preserve one source of truth for the lock budget and apt options** — *firm* — rationale: issue #123 explicitly requires reuse rather than a copied solution.

## Open questions

- **Where should the canonical policy live?** — proposal: evaluate generated source, an embedded data file, or a Go-owned renderer for the shell fragment. *(proposal, not committed)*
- **Does the workspace stage need the full base-layer retry behavior or only apt's DPkg lock option?** — proposal: settle this from the exact locks exercised by install versus update before choosing the data model. *(proposal, not committed)*
- **Should malformed policy fail at generation/build time or startup?** — proposal: prefer generation or tests so a formatting change cannot create a package-init panic in a shipped binary. *(proposal, not committed)*

## Suggested next steps

1. Run `/grill-me` on the ownership boundary and the exact lock semantics required by issue #123.
2. Compare a generated Go/shell pair, a tiny embedded data artifact, and a Go-owned bootstrap renderer.
3. Choose the representation that deletes `parseAptLockArgs`, regex parsing, and package-init panic.
4. Keep behavior tests for the base-layer script and workspace apt invocation at their public boundaries.
5. Re-run `make check`, `go build ./...`, and bootstrap/workspace package tests.

## Key references

- https://github.com/byranZA/smith/issues/123
- `internal/bootstrap/apt.go`
- `internal/bootstrap/bootstrap.sh`
- `internal/bootstrap/apt_test.go`
- `internal/workspace/converge.go`
