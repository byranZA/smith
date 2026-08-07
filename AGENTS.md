# smith

Smith turns a fresh VPS into a ready-to-use remote development machine and manages coding agents across the repositories on it. The user brings the machine; smith provisions it, then runs the delegation loop for the coding agents it manages.

The `coding-standards` skill (`.claude/skills/coding-standards/`) covers *how* to write Go in this repo. This file is the *why* and the boundaries.

## Communication

Treat your reasoning, planning, and internal terminology as completely invisible to the user. Your visible response must stand alone: introduce every necessary concept before using it, state conclusions directly, and include only information that helps the user understand the result or take the next action. Do not narrate your reasoning process, preview later sections, repeat conclusions, or refer to concepts that have not been defined in the visible conversation.

Communicate like an engineer handing completed work to another engineer: say what changed, what matters, and what remains uncertain, using the fewest words that preserve the necessary meaning.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `byranZA/smith`, managed with the `gh` CLI. External PRs are not a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default label strings, all present in the repo. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
