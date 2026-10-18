# Attach defaults by driver, and says when it is read-only

> Amends [ADR-0005](./0005-terminal-rides-the-ssh-door.md), which had `session attach` default to read-only for every session, split on intent rather than driver.

## Context

ADR-0005 made `attach` read-only unless `--interact` is given, to guard against typing into an agent's session by reflex. When it was written, a human-driven session was the only kind smith created, so the default guarded nothing it actually ran. On the first real box run, the operator attached to their own human-driven session and typed, and nothing happened. tmux listed the client as `readonly=1`, but nothing in the terminal said so, and the operator concluded the box had frozen ([issue #311](https://github.com/byranZA/smith/issues/311)).

Since [ADR-0017](./0017-the-loop-starts-its-own-session-on-a-box.md), a session also has a recorded driver: a human (`session start`) or the loop (`loop run <box>`). Read-only protects only the second.

## Decision

**`session attach` takes its default mode from the session's driver.** A human-driven session attaches writable, and a loop-driven one attaches read-only. `--interact` still connects writable to either. A worktree with no session record reads as human-driven, as it already does in `session list`.

**A read-only attach always says so, and says how to type.** Before handing the terminal to tmux, `attach` prints one line naming the read-only state and `--interact`. While attached, the read-only state stays visible in tmux itself, because a line printed before attaching scrolls away under tmux. The marker is per client, so a writable client on the same session does not show it.

## Considered options

- **Keep read-only for every session and only make it visible** — rejected. It fixes the confusion but keeps a guard on the one kind of session that has nothing to guard, so the operator's most common attach stays a two-step.
- **A flag or config setting for the default** — rejected. The driver already says whether there is an agent to protect, and smith sets the driver from the verb, never from a flag.

## Consequences

- **Driver and attach mode are no longer orthogonal.** The driver picks the default and `--interact` overrides it. A loop-driven session can still be watched read-only or, when it needs a hand, jumped into.
- **Read-only is still advisory** (ADR-0005). Nothing here makes it a permission boundary.
- **The failure mode of a read-only attach changes from silent to stated.** Typing into a loop-driven session still does nothing, but the terminal says why and how to reconnect writable.
