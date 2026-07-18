# Ralph loop

Autonomous loop that works a spec (PRD) GitHub issue to completion, one child
task at a time. It pairs with the global `/to-spec` → `/spec-to-tasks` →
`/review-and-task` workflow: a spec issue with a `## Tasks` checklist of child
issues, each labelled `ready-for-agent` (AFK) or `ready-for-human` (HITL) and
listing its `## Blocked by` dependencies.

## Usage

```sh
# Loop over every AFK task under spec issue #42
python ralph/ralph.py 42

# Bound the run and the per-task retries
python ralph/ralph.py 42 --max-iterations 20 --max-attempts 2

# Just show what would run next, then exit
python ralph/ralph.py 42 --list

# Human-in-the-loop: pick the next available task and run ONE interactive
# session on it, then exit (successor to the old ralph-once.sh)
python ralph/ralph.py 42 --once

# Use Codex instead of Claude Code as the backend (headless or --once both work)
python ralph/ralph.py 42 --agent codex
python ralph/ralph.py 42 --agent codex --once
```

Prerequisites: `gh` authenticated (`gh auth status`) against this repo, Python
3.11+, and the CLI for whichever backend you use (`claude` by default, or
`codex` with `--agent codex`) on PATH.

## Agent backends

Claude Code is the default; Codex is an alternative, selected with `--agent`.
Each has a headless (autonomous) and an interactive (HITL, `--once`) mode:

| | Headless | Interactive (`--once`) |
|---|---|---|
| **claude** (default) | `claude --print` with `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` | `claude` attached to the terminal |
| **codex** | `codex exec --yolo` (bypasses approvals + sandbox) | bare `codex` TUI (approvals on) |

`--model` defaults to `opus` for claude and to codex's own configured default
for codex; pass `--model` to override either. `--permission-mode` applies to
claude only.

## How it works

Each iteration the **driver** (not the prompt) decides what happens:

1. Query GitHub for the spec's children and their live state.
2. Pick the first task that is **open**, `ready-for-agent`, and **unblocked**
   (all its blockers closed). Dependency order comes from the spec's `## Tasks`
   checklist; fix issues filed by `/review-and-task` mid-run (`Part of #42`)
   are picked up automatically on the next iteration.
3. Run one headless `claude` session on exactly that issue, using
   [`ralph-prompt.md`](ralph-prompt.md).
4. Re-check the issue: **closed** = done; **still open** = the agent got stuck
   and left a `WIP:` comment. After `--max-attempts` failed tries a task is
   skipped so the loop doesn't spin on it.

The loop **exits when no AFK task is available** — spec complete, or only
human/blocked/skipped tasks remain — or when `--max-iterations` is hit. There
is no `COMPLETE` sentinel; the driver owns the exit decision.

The headless sessions run with `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` so no
long-running background shell lingers between iterations.

## Human-in-the-loop (`--once`)

`--once` is the successor to the old `ralph-once.sh`: the driver still selects
the next available task for you, but instead of a headless run it launches a
normal **interactive** claude session attached to your terminal so you can
steer it, answer questions, and make the calls an agent shouldn't make alone.
It does one task and exits — no iteration, no auto-skip.

## Files

- `ralph.py` — the driver (the one script you run).
- `ralph-prompt.md` — the per-iteration prompt. The driver substitutes the
  chosen issue number/title and the spec number before each run.
- `ralph.log` — output of the most recent run (git-ignored, rewritten each run).
