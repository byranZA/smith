# The repo file — a per-repo layer for the loop, agent adapters as code, and GitHub by name

> Amends [ADR-0006](./0006-the-blueprint-config-surface.md).

## Context

The box side of smith has been built first: setup, the blueprint, sessions, the relay. The loop is being built from the other end. It replaces `ralph/`, a Python driver that works a spec's tasks one at a time through a coding agent, so that smith can build smith. The two halves are meant to meet when an agent-driven session on a box runs the loop as its root process ([ADR-0008](./0008-smith-runs-on-the-box.md)). Until then the loop runs wherever smith is invoked, in the repo it is invoked from.

Three earlier positions stand in its way:

- **"One home, not a layered search path."** ADR-0006 says smith never discovers a repo root and that where the operator stands has no bearing on what a command does.
- **ADR-0006's forecast.** It said the AFK map's `agent` block would be "fixed-key command references" and would land in preferences.
- **Credential-agnostic.** smith "knows no service by name — not GitHub."

## Decision

**A repo gets one committed, optional file, `.smith/repo.yaml`, holding the loop's agent, model and effort.** Agent settings are a property of the *repo*: everyone who runs the loop on smith should get the same agent unless they say otherwise. They are not a property of the operator (preferences) or of a kind of box (a blueprint). Precedence extends the existing field-level chain: **flag > repo file > built-in default**. A preferences slot between the repo file and the default is reserved but not built, because nothing yet needs an operator-wide agent default. Flags (`--agent`, `--model`, `--effort`) override a single run without editing the file.

The repo file is YAML and strictly validated, for the reason ADR-0006 chose YAML over TOML: every unknown key is reported with a line number, and a file that will keep growing is where a silent typo hurts most.

The directory is `.smith/` at the repo root, matching the config home's name. When a repo's `.smith/` *is* the config home (a home directory kept in git), `smith repo init` and the loop refuse rather than mixing operator config and repo config in one directory.

The single-home rule survives for every other verb. Only the loop and `smith repo` read the repo root. Plain `smith init` keeps scaffolding the config home and does not change behaviour based on where it is run.

**Agent adapters are code, not data.** The forecast's "command references" would have made them data, like the provider adapter ([ADR-0007](./0007-the-provider-adapter-is-data.md)). The cases differ. There are many providers, and each differs only in the shape of its JSON. There are few agent CLIs, and they differ in behaviour: how to run headless and how to run interactive, what environment to set, how effort is spelled. `claude`, `codex` and `pi` ship built in, and configuration names one. An unattended run is always fully autonomous (claude headless without approval prompts, `codex exec --yolo`, and `pi`, which never prompts), so there is no permission setting. The model is passed verbatim, and effort is a smith-owned scale each adapter translates.

**The loop knows GitHub by name.** v1's tracker is GitHub Issues through `gh`: sub-issues as a spec's tasks, `## Blocked by` as dependencies, and the `ready-for-agent`, `ready-for-human` and `spec` labels hardcoded. smith uses `gh`'s own authentication and holds no credential, so credential-agnostic still holds where it was about credentials. The exception covers the loop alone and is kept behind one tracker seam, so a second tracker, and labels set in the repo file, can be added later without reshaping the loop.

**The loop prompt is built in, and a repo may eject it.** With no `.smith/prompt.md`, the loop uses the prompt compiled into smith, so a repo that wants the default gets the latest one with every upgrade. Ejecting writes the prompt into the repo, which owns it from then on. smith never overwrites it, and printing the built-in is how an ejected repo diffs and catches up.

## Considered options

- **Agent settings in preferences (ADR-0006's forecast).** Rejected because a teammate running the loop on the same repo would get a different agent depending on whose machine they used.
- **TOML for the repo file.** This is the convention elsewhere and was the user's first instinct. Rejected for the same strict-validation reason ADR-0006 gives, and for one format across the product.
- **A data-driven agent adapter, like the provider adapter.** Rejected: the variation between agents is behaviour, and describing behaviour as data would mean a small language for it.
- **A data-driven tracker adapter now.** Rejected as premature. There is one real tracker, and the seam leaves room for a second.
- **Context-sensitive `smith init`.** Rejected because it would make the one verb that scaffolds the config home depend on where it is run, which is the property ADR-0006 exists to keep.

## Consequences

- `ralph/` can be deleted once the loop reaches parity. Smith's own Go-specific prompt moves to smith's `.smith/prompt.md` as an ejected prompt.
- Every later repo-scoped setting (task labels, the tracker, limits) has a home that already exists, and adding one is not a new layer.
- When the loop runs on a box, the repo file travels with the repo. The blueprint stays responsible for the box and the repo file for the work, so neither needs fields from the other.
