# A JSON document is a versioned contract, even before v1

> Narrows the `v0.x` position in [docs/install.md](../install.md#compatibility), that there is no compatibility promise, for one surface: the documents the read verbs print under `--json`.

## Context

An agent working beside the operator asks smith what boxes exist, whether one is healthy and what the loop will do next, and until now the only answer was prose smith may reword in any release. [#201](https://github.com/byranZA/smith/issues/201) had already decided prose is not a wire format for the relay. [#610](https://github.com/byranZA/smith/issues/610) gave the six read verbs `--json`, and left one question to settle: a `schema_version` in each document, or rely on the release tag while smith promises no compatibility.

## Decision

**Each document carries its own `schema_version`, and the document, not the prose, is what a program may rely on.** `smith help json` publishes the shapes and this policy, built into the binary so it matches the smith installed.

- **Adding a field is free.** A document may gain a field in any release, with no bump. A program ignores keys it does not know.
- **Renaming or removing a field is breaking.** It bumps that document's `schema_version`, and the release notes announce it. A break is allowed before v1, but never silently.
- **A new token is breaking too.** So is renaming or removing one, in a field that holds a fixed set of tokens, such as a verdict, a task state or an outcome. A new field can be ignored, but a new token arrives where a program is already branching, so a program that knows only the old set could misread it. The bump lets the program stop instead.
- **Versioned per document.** The version starts at 1 and each document moves on its own, so a break in `machine status` says nothing about `loop list`.
- **Branch on tokens, never wording.** A value a program would branch on, such as a verdict, a task state or an origin, is a fixed token kept apart from the prose an operator reads.
- **Prose stays free.** Any verb's wording may change in any release.

## Considered options

- **Rely on the release tag.** Rejected: a program would have to know which smith releases changed which shape, and it can already read a version from the document it is parsing.
- **A new token as a free addition, like a new field.** Rejected: it would spare a bump, but a program branching on the old set gets no signal that a value it cannot handle exists.
- **A compatibility promise beyond this policy.** Out of scope before v1. smith is still free to change a shape, and owes the reader only a signal when it does.

## Consequences

- Each shipped shape is a public surface: a rename costs a bump and a release note, so a field name is worth getting right when it lands.
- The release audit reads a document's change like any other user-visible one, and a rename in the range belongs in the notes' upgrading section.
- A relayed `session list` needs no negotiation between versions, because the relay already refuses a version mismatch and copies the box's document unchanged.
- A feature that adds a verdict, task state, outcome or origin also bumps every document that carries it, so a new loop outcome bumps `session list`.
