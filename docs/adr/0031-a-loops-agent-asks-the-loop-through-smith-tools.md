# A loop's agent asks the loop through smith tools

> Amends [ADR-0027](./0027-the-loop-runs-the-repos-stack.md), which ran the recorded `up` only before an attempt and the review. Adds a channel to the file-based ones of [ADR-0025](./0025-the-loop-files-the-reviews-findings.md) and [ADR-0030](./0030-the-loop-posts-each-attempts-report-on-its-task.md).

## Context

ADR-0027 gives smith the repo's stack: it runs the recorded `up` before every attempt and the review, and `down` when the run ends. Inside one attempt the loop's agent has no sanctioned way to bring the stack up again. A changed `requirements`, `package.json`, `Dockerfile`, compose or env file needs `up` re-run, and that happened only at the next attempt. Agents on intraday-breakout worked around the gap by running `docker compose up -d` themselves, which smith cannot see and which carries none of the worktree's identity that parallel slots will need ([#468](https://github.com/byranZA/smith/issues/468)).

The agent guide and the `.smith/README.md` scaffold also say that an agent handed a task by the loop does not run smith. No ADR holds that rule. Until now the loop's agent has talked to smith only through files smith reads after the agent exits: the findings file (ADR-0025) and the attempt report (ADR-0030). See [#792](https://github.com/byranZA/smith/issues/792) and the grill in [#791](https://github.com/byranZA/smith/issues/791).

## Decision

**The loop's agent, working a task or the review, runs smith only through smith tools: a closed, named set of actions it asks the running loop to perform, with `smith tool <name>`.** This replaces the unwritten rule that it does not run smith. The first tool is `stack-up`, which re-runs the stack's recorded `up`.

- **The agent asks, the loop acts.** The loop performs the action in its own process, for that run and that worktree. The agent does not run `up` itself.
- **A per-launch socket.** For each agent launch, task attempt or review, the loop serves a Unix socket and passes its path to the agent in `SMITH_TOOLS`. The socket lives in a 0700 directory under the state home, with a random file name, and the loop closes it when the agent exits. Outside a run, or with a dead socket, `smith tool` refuses.
- **The whole stack.** `stack-up` re-runs the recorded `up`, with no service argument and no `restart:` key. smith still knows no service by name, and `up` is already required to be safe to re-run and ready when it returns. It does not compare against the worktree's repo file, rewrite the record or run `down`, so an edited `up` takes effect at the next attempt, as ADR-0027 has it. The loop keeps the lease, so nothing is handed over.
- **Discovery and visibility.** The work and review prompts list only the tools this run serves, and `smith tool list` prints the same list. The operator sees each call on the loop's terminal, and the attempt report's header counts re-ups.
- **A failed tool does not end the attempt.** The agent sees the output and an exit status, 1 when the tool ran and failed, and 2 when it was refused before anything changed.
- **Who owns data changes.** The repo's `up` owns migrations and should apply them to head, so a migration added mid-attempt lands with `stack-up`. A destructive rebuild is the repo's own reset command, run against the running stack. It is not starting or stopping a service.
- **Windows 10 1803 and later** is the requirement, as the first Windows with Unix sockets. There is no fallback.
- **Growth.** Tools a repo declares or turns off in its repo file are the intended growth. The registry, the generic protocol and the reserved built-in names leave the seam for them. A `--fresh` re-up that runs `down` first, for a rebuild that must destroy volumes, is a later candidate.

## Considered options

- **C: the agent runs `up` itself.** Rejected: the lease and the in-memory record live in the loop process, the agent's own `up` would carry no worktree identity, and smith could not see the call or record it.
- **A token in the environment, or a file the loop polls.** Rejected: a socket gives identity by construction, because the loop knows which worktree, and later which slot, each socket serves and the client claims nothing, so a neighbouring slot cannot be touched. Its life ends with the attempt, and it streams output and returns a status where a polled file cannot.
- **A service argument or a `restart:` key.** Rejected: it would make smith know services by name, and `up` already covers it.
- **Leaving the rule as it was.** Rejected: agents broke it anyway, with nothing smith could see.

## Consequences

- The rule in the agent guide, the `.smith/README.md` scaffold and the glossary becomes: an agent handed a task by the loop runs smith only through the smith tools its prompt lists.
- `.smith/README.md` is repo-owned, so an existing copy keeps the stricter old line, "does not run smith". It stays true of the agent's behaviour but forbids the tool until the repo updates it. The release notes say how.
- A state home whose path would make the socket path too long, about 104 bytes on macOS and 108 on Linux, makes the run refuse before it marks any task.
- A repo prompt ejected before this lacks the `{{SMITH_TOOLS}}` placeholder. The run prints a notice, and the tools still work.
- Code reloading stays the repo's job.
