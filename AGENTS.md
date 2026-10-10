# smith

Smith turns a fresh VPS into a ready-to-use remote development machine and manages coding agents across the repositories on it. The user brings the machine; smith provisions it, then runs the delegation loop for the coding agents it manages.

The `coding-standards` skill (`.claude/skills/coding-standards/`) covers *how* to write Go in this repo. This file is the *why* and the boundaries.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `byranZA/smith`, managed with the `gh` CLI. External PRs are not a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default label strings, all present in the repo. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Working a task

These hold for every agent working a task here, in the smith loop or not.

- **Checks:** `make check` before handing work back, and `make race` for anything concurrent. A green `make check` can hide a skipped lint: the `coding-standards` skill's `GO.md` says how to read it.
- **Commits:** Conventional Commits that reference the issue, such as `feat: <summary> (#123)`.
- **Guardrails:** `.claude/` (settings and skills) and `.smith/` (below) change only in a task that names them.

## `.smith/` is production

`.smith/` at this repo's root holds the repo file and loop prompt the loop runs *this* repo with: live configuration, not a fixture or a scratch area. Feature work, tests and examples build their own `.smith/` in a temp directory. The committed one changes only in a task that names it, and that task is HITL.
