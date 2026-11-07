# The loop runs the repo's stack, one run at a time

> Amends [ADR-0019](./0019-worktree-preparation.md), which kept services out of worktree preparation.

## Context

A repo whose checks need services, such as a database in Docker, had no way to tell smith about them. Each agent improvised: in a different order each attempt, and one left about 12 containers running, which the loop's stop could not reach because they belong to the Docker daemon and not to the agent. Two runs of one repo on a box also collide on fixed ports and container names. ADR-0019 left services to the blueprint, which cannot start a database per attempt. See [#697](https://github.com/byranZA/smith/issues/697).

## Decision

**The repo declares a stack, and smith owns its lifecycle.** `stack:` in the repo file holds `up` and `down`, each one non-empty string, both required. smith knows no service by name.

- **Lifecycle.** smith runs `up` with `sh -c` in the worktree, under `mise exec`, after preparing it and before every attempt and the review. `up` must be safe to re-run and must not return before the stack is ready. It has no timeout, like `prepare:`. A failed `up` stops the run `failed` and counts as no attempt. `down` runs however the run ends, on a context that survives Ctrl-C and SIGHUP, before the stop report is posted and before a finished worktree is removed, with a fixed 5-minute timeout. A failed or hung `down` is a warning and the stop report says the stack did not stop; the run keeps its outcome.
- **The recorded `down` runs.** It pairs with the `up` that ran. Before each `up`, if the worktree or either command differs from the record, the recorded `down` runs first. A task that lands a repo file with no stack takes the running stack down.
- **The lease.** One record per repo in the state home holds the worktree, work branch, both commands and the start time. The run holds an advisory OS file lock on it. The kernel releases the lock on any death, SIGKILL and reboot included, so a locked file is a live holder and an unlocked file with a record is stale. That avoids PID reuse. The record is written before `up` and deleted only after `down` succeeds, so a run killed mid-`up` is recoverable. It is keyed by the normalised `origin`, so two clones share a lease.
- **Refusal and recovery.** A second run of the repo is refused, naming the holder's work branch and worktree. Preflight refuses before any task is marked; the first `up` enforces, which covers a branch that adds a stack. At the start of any run of the repo, the lease free, a stale record's `down` runs in its recorded worktree. A failed recovery warns and the run goes on, and `up` fails loudly if something still holds a port.
- **Off by default off a box.** smith cannot see a stack the operator started by hand, and for a repo like skein `up` resets the seeded dev database. So the stack runs on a box, where only smith runs it, and off a box only when the run passes `--stack`. `--stack` is refused with a box, which always runs its stack. "On a box" is the staged-tree test of [ADR-0023](./0023-local-task-worktrees-take-placements-from-the-checkout.md).
- **A stack never runs nested in a container box.** It needs Docker-in-Docker or a mounted Docker socket, which smith exists to avoid. A socket mount also fails for bind mounts, as the host daemon cannot see the box's worktree paths, and `127.0.0.1` in the container does not reach the published ports. smith neither installs Docker there nor detects a container box: it fails at `up` with the usual stack failure. This is a documented limit and is not enforced.
- **Docker stays in the blueprint.** The packages and the `docker` group come from it ([ADR-0019](./0019-worktree-preparation.md), [ADR-0026](./0026-published-docker-ports-default-to-loopback.md)); a failed `up` says so.

## Considered options

- **Per-worktree isolation first.** Rejected for now: mapping worktree identity onto container names and ports is repo-specific and large ([#546](https://github.com/byranZA/smith/issues/546)). A serial lease gives a repo with a stack one task at a time and removes the collisions without it. Isolation can build on the lease later.
- **A PID in the record.** Rejected: PIDs are reused after a reboot, and the OS lock needs no liveness check.
- **A `cleanup:` command run by `session stop`.** Rejected: it covers only sessions, and the lease and recovery cover every end of a run ([#650](https://github.com/byranZA/smith/issues/650)).
- **The stack on by default off a box.** Rejected for the reason above.
- **A health-check key.** Rejected: `up` is responsible for not returning until the stack is ready.

## Consequences

- A repo with a stack works one task at a time; a second run waits for the first.
- A converge that restarts the Docker daemon (ADR-0026) still does not detect a running stack.
- Images and volumes build up on a box and smith does not prune them.
- Human sessions do not touch the stack, because the lease cannot see what a human started.
