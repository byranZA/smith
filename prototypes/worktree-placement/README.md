# PROTOTYPE — worktree + env/seed placement spike (smith issue #57) — THROWAWAY

Proves smith's **workspace materialisation**: clone a repo once, cut N git
worktrees, and materialise files into each from the blueprint — then re-run to
show the two placement modes behave. Not production code; lives only on its
capture branch, deleted from `main`.

## Run

```sh
./spike.sh run       # full guided demo: setup → pass 1 → mutate → pass 2 → verdict
./spike.sh status    # show the worktree tree + placed-file contents
./spike.sh free      # re-run the placement pass only (hand-edit files between runs)
./spike.sh down      # delete the scratch world
```

No network, no provider: the "upstream repo" is created locally under `.run/`
(gitignored). Needs only `git` and `bash`.

## The question this settles

Is this the right **placement primitive**?

```
source-reference  →  destination-path   [ mode: converge | once ]
```

- **source-reference** — a `scheme:arg` string, **reference not value**, the same
  shape as `internal/secret` (`file:/path`, `env:VAR`). The blueprint points *at*
  the content; it never embeds it.
- **destination-path** — relative to each worktree root.
- **mode**
  - **converge** — overwrite every run; the source is desired state. The `.env`
    case. No-op when content already matches (check-before-change).
  - **once** — write only if absent; the worktree owns it after. The seed case.
    A hand-edit or agent-write survives every later pass.

## What the spike demonstrates

Pass 1 seeds three fresh worktrees. Then, between passes, it **changes the env
source** (`FEATURE_FLAG off→on`, rotates a token) and **hand-edits `seed.sql`
inside `session-2`** — exactly the drift a running session produces. Pass 2:

- `converge` re-materialises `.env` / `.env.ci` in **every** worktree from the
  changed sources.
- `once` leaves `session-2`'s hand-edited `seed.sql` **untouched**.
- worktree isolation holds: a commit on `session-1`'s branch is invisible to
  `session-2`.
- perms are per-placement: `.env` lands `600`, `seed.sql` `644`.

## Design locked in #57

The primitive is `source-reference → destination-path [mode]`, and:

1. **Two modes, and only two, for v1** — `converge` (desired-state wins,
   overwrite every pass, no-op when content matches) and `once` (write if
   absent, then the worktree owns it). No `template`/interpolation, no `skip`.
2. **`once` identity is path existence** — present ⇒ preserved; delete ⇒
   re-seeded next pass. No separate "already seeded" marker in v1; "delete to
   reset" is the affordance.
3. **Shared scheme grammar, separate resolver** — placement reuses the
   `file:`/`env:` vocabulary of `internal/secret` but not `secret.Resolve`: it
   resolves to **whole-file bytes** (no whitespace-trim), where secrets resolve
   to a trimmed value.
4. **Perms are declared per placement** (e.g. `.env` → `600`, `seed.sql` →
   `644`) in the blueprint line, never inferred from the destination name.
5. **File-level placement only in v1** — directory/tree placement is out for now
   (left to the fog).
