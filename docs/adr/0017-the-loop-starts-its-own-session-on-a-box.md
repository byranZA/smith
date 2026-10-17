# The loop starts its own session on a box

> Amends [ADR-0008](./0008-smith-runs-on-the-box.md), which deferred `session start --driver agent` and had smith report liveness only, and [ADR-0016](./0016-the-loop-comments-on-a-task-it-skips.md), which had the loop comment only on a task it skips.

## Context

ADR-0008 planned an agent-driven session as `session start --driver agent`: the session verb would create the session and the loop would be its root process. Since then the operator has learnt to run the loop locally with `smith loop run <spec>`. The natural way to run the same work on a box is to point that command at the box, not to learn a second command that takes the loop's arguments.

A loop on a box is AFK by definition, so it can't run in the foreground of the operator's SSH connection: closing the lid would end it. And once the session ends, so does the loop's report, which today is only printed to stdout.

## Decision

**`smith loop run <box> <spec> --repo <repo>` runs the loop on the box as the root process of a new session, then returns.** Sessions get their driver from the verb that creates them, never from a flag: `session start` makes a human-driven session, `loop run <box>` a loop-driven one. `session start` has no `--driver`.

- **On the box, `--repo` chooses a session run.** The relay sends `smith loop run <spec> --repo <repo> …`, which creates a loop-driven session in that repo's clone and detaches. Without `--repo` the loop runs in the foreground in the current directory, as it does locally. Locally, `--repo` is refused.
- **Preflight runs in the foreground before the session starts.** It covers settings, the agent lookup, reading and admitting the spec through `gh`, and refusing when the branch's session is live. Its output streams back to the operator, so a mistake fails at the terminal, not in a session nobody is watching.
- **The loop records its outcome.** When it stops, it writes the outcome where `session list` can read it. `session list` gains DRIVER and OUTCOME columns: `complete`, `waiting on human`, `cap reached`, `attempts exhausted`, `stopped` or `failed`, and `unknown` for a loop-driven session that ended without a record. smith reports the outcome the loop wrote down, never a process's exit status.
- **The loop posts its stop report as a comment on the spec**, on every run, local or on a box. The report can be read from anywhere, and GitHub notifies whoever is subscribed to the spec. A failed comment write is a warning, as in ADR-0016.
- **The loop treats SIGHUP like SIGTERM.** `session stop` hangs up the session's terminal, so it ends a run as cleanly as Ctrl-C: the agent is interrupted, its label removed, and the outcome is `stopped`.
- **One session per branch, on one box.** A live session on the branch refuses the run. A stopped one hands over its worktree and work branch, which is how a spec moves between the loop and a human for its HITL tasks.
- **`--interactive` is refused with a box.** A human session with the command typed by hand covers that case.

## Considered options

- **`session start --driver agent --spec N`** (ADR-0008's plan). Rejected: it duplicates every loop flag on the session verb, and it splits one piece of work across two commands depending on where it runs.
- **A plain relay of `loop run`.** Rejected: a foreground run over SSH ends when the connection does.
- **A full per-session log** (tmux `pipe-pane`) instead of a recorded outcome. Rejected for now: it is more than "did it finish?" needs.

## Consequences

- Refusing a second run of the same spec on another box needs a real claim with an owner and a lease. That is left to [#432](https://github.com/byranZA/smith/issues/432); ADR-0015's start-up sweep would strip a live run's labels on another box.
- `session start` is free for its own future, such as a resident watcher that starts loops as specs become ready. That would need its own term, since it is not a session.
