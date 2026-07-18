# smith — Design Rules

Smith turns a fresh VPS into a ready-to-use remote development machine and manages
coding agents across the repositories on it. The user brings the machine; smith
provisions it, then runs the delegation loop for the AFK ("away from keyboard")
coding agents it manages.

The `coding-standards` skill (`.claude/skills/coding-standards/`) covers *how* to
write Go in this repo. This file is the *why* and the boundaries.

## Design boundaries (hold these)

- **Deterministic core, LLM only at the edge.** Smith itself is a deterministic
  state machine — provisioning, task selection, state transitions, recording. The
  only LLM in the run loop is the worker agent doing the coding. Keep it that way:
  no LLM calls buried in smith's control flow.
- **git is fundamental; smith does not reinvent it.** Smith records what happened; it
  does not manage branches, merges, or worktree topology. The user's normal git
  workflow is never rewritten underneath them.
- **The project owns its runtime; smith owns the loop, state, prompt, and CLI.**
  Smith never assumes how a project builds, tests, or runs — that's the project's
  (and the VPS's) business.
- **The VPS is the sandbox.** Isolation is the machine the user provisioned, not a
  container smith manages. Smith's job on that box is to set it up and then run and
  record the loop.
- **Single static binary, fast startup.** Smith cross-compiles to one dependency-free
  executable with no long-running host daemon. Keep the CLI path lean.
