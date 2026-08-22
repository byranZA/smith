> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Blueprint document model

**Date:** 2026-08-22
**Source session:** review-and-task on #77 (`feature/blueprint-config-surface-77`)

## One-line summary
Quality review of the blueprint config surface found two related structural moves: validate one positioned YAML document instead of two representations, and stop copying the fixed-key schema across Blueprint / Preferences / Effective.

## Goal
**Immediate:** grill whether these restructurings are worth doing before the next box-lifecycle slice consumes `Parse` / `Resolve`.
**Broader:** later slices (provider adapter, staging, workspace) should inherit one document model, not three copies and a validation pipeline that already dropped `git` from the resolved value.

## Context the next agent needs
Slice 1 of the box-lifecycle map shipped the reading half of `~/.smith/`: `internal/blueprint` (strict YAML + semantic rules), `internal/config` (home, Select/Load, Resolve), and `smith blueprint check`. Spec is #77; ADR-0006.

`Parse` is a deep public surface (bytes in, `Blueprint` or `*ValidationError` out). Internals are a pipeline of representations:

1. `decodeStrict` unmarshals to `yaml.Node`, then `Decode`s the same bytes again with `KnownFields(true)`.
2. Decoder `TypeError`s are regex-translated and re-pathed through `pathIndex` (`strict.go`).
3. Reserved names walk the Node because `tools`/`env` are open maps `KnownFields` cannot see (`reserved.go`).
4. `validate` walks the typed `Blueprint` and drops line numbers (`semantics.go`, `reference.go`).

`Preferences` restates Blueprint’s fixed keys. `Effective` restates four of them as `Value`/`Adapter` and **omits `git`**, which is on both inputs and in the worked preferences example. `gitFindings` therefore runs on one document, so preference identity plus a blueprint placement to `~/.gitconfig` composes into two writers for the same file.

Scoped follow-ups for the *behaviour* gaps (print git/collections; run the gitconfig exclusion after resolve) were filed as sub-issues of #77. This hand-off is the *structural* answer that would make those gaps hard to reintroduce.

Inspected and not flagged: `homeResolver` as a func type is the narrow Go contract; YAML tags on internal structs are how `Decode` works (a parallel DTO would be an identity wrapper); `secret.Split` is already reused.

## What was discussed
Two code-judo moves, not local cleanup:

**One positioned walk.** Unmarshal to `yaml.Node` once. Walk it for unknown fields, reserved open-map keys, references, and semantic rules so every `Finding` carries `Line` and `Path`. Decode the node into `Blueprint` only after. That deletes `pathIndex`, the `TypeError` regex translator, and `reserved.go` as a second walker. The current file split (`strict.go` / `reserved.go` / `semantics.go` / `reference.go`) is that pipeline, not a decomposition of one idea.

**One shared shape for precedence fields.** Extract the fixed-key fields into one type embedded by `Blueprint` and `Preferences`. Resolve into that type plus an origin map (or a single resolved value that cannot omit a field the inputs share). Run the gitconfig exclusion on the resolved git+placements pair, not on one document. A later setup path will otherwise invent a second resolver or keep shipping an incomplete `Effective`.

These interact: the walk produces the typed document; the shared type is what gets resolved. Grilling them apart risks a second copy of the schema.

## Decisions made
- **Scoped behaviour fixes are auto-issued; this restructuring is not** — *firm* — review-and-task routes ambitious quality findings to a grill session, not an AFK task.
- **`Parse` staying bytes-in / document-or-report-out is not in question** — *working assumption* — the public interface is already deep; the proposal changes how it is implemented.

## Open questions
- **Is a single Node walk actually simpler against `go.yaml.in/yaml/v3`?** — proposal: spike whether `KnownFields` plus line numbers can be recovered from the Node without the TypeError regex. If the library will not give unknown keys with positions except via `Decode`, the dual parse may be the cheapest honest design. *(proposal, not committed)*
- **Shared type vs origin sidecar.** Embed a `Fixed` struct in both documents and resolve into `Fixed` + `map[field]Origin`, or keep per-field `Value` structs? The sidecar cannot drop `git` by forgetting a field in a third struct. *(proposal, not committed)*
- **Where does the gitconfig exclusion live after resolve?** Inside `Resolve` (config then depends on a semantic rule) vs a function that takes the resolved git + blueprint placements (keeps Parse's intra-document check and adds a composition check). *(proposal, not committed)*
- **Do later slices need this before they consume the loader?** If slice 2 (provider adapter) only reads `Provider`, the incomplete `Effective` may not bite yet. Slice 5 (staging) and 6 (workspace) will.

## Suggested next steps
1. `/grill-me` on the Open questions — especially whether the Node walk is simpler in this YAML library, and whether the shared type is an embed or an origin sidecar.
2. Read `internal/blueprint/strict.go`, `reserved.go`, `semantics.go`, `blueprint.go` `Parse`/`decodeStrict`, `internal/config/resolve.go`, `internal/blueprint/preferences.go`.
3. If the grill keeps the dual parse, record why in an ADR note so the next reviewer does not re-propose it.
4. If it keeps the shared type, do that before wiring `machine setup` into the precedence chain — that is the second consumer that will otherwise copy `Effective`.
5. Do not start from this doc as a task. Scoped issues already cover printing git/collections and the cross-document gitconfig exclusion.

## Key references
- Spec: https://github.com/byranZA/smith/issues/77
- ADR-0006: `docs/adr/0006-the-blueprint-config-surface.md`
- `internal/blueprint/blueprint.go` (`Parse`, `decodeStrict`)
- `internal/blueprint/strict.go`, `reserved.go`, `semantics.go`
- `internal/config/resolve.go` (`Effective`, `Resolve`)
- `internal/blueprint/preferences.go`
- Worked example: `docs/examples/preferences.yaml` (declares `git`)
