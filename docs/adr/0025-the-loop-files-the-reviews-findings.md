# The loop files the review's findings, and the agent only reports them

> Amends [ADR-0015](./0015-the-loop-marks-tasks-as-a-signal-not-a-claim.md): `smith:reviewed` is a loop-owned mark like the others. Amends [ADR-0022](./0022-presets-and-roles.md): `review` is a second role, a task's `preset:` label never chooses it, and a pinning run flag pins it with the rest of the run.

## Context

A review turns its findings into tasks, one tracker call per finding, and any call can fail partway: auth, the network, a rate limit, or an old `gh`. A task's agent drives the tracker itself, and the loop only checks the result afterwards. Copying that for the review leaves a half-filed review that nothing can resume safely: a retry runs fresh subagents whose findings are worded differently, so it files duplicates, and whether a resume works at all depends on the agent following a resume protocol in its prompt.

## Decision

**The review agent writes its findings to a findings file and touches nothing else. The loop files them, keeping a ledger on the tracker so any later run on any machine resumes the filing instead of reviewing again.**

- **The findings file** is JSON at a path the loop passes in `{{FINDINGS_FILE}}`, under `.git/smith/`, deleted before each attempt. Each finding has an axis, severity, title, who, body, and optional `blocked_by` naming other findings by position; a `not_filed` list holds the restructurings too large for one task. The loop gives each finding its id from file order. A file is valid or rejected whole.
- **An attempt succeeds** when the agent exits 0 and leaves a valid file. A missing or invalid file is a failed attempt, under `MaxAttempts` and ending in `review failed` as before.
- **The ledger** is the review comment, posted by the loop before it files anything: the readable review followed by a hidden `<!-- smith:review … -->` block holding the findings and the work branch sha reviewed. The loop files each task with its finding id in the body's Source line, edits the comment once to name the tasks, then adds `smith:reviewed`.
- **A `gh` failure while the loop files** is a tracker failure: the run stops at once, without retrying, and ends `review incomplete`, an ending apart from `review failed` so the operator can tell a `gh` problem from an agent or prompt problem.
- **A run that finds a ledger and no `smith:reviewed`** skips the agent and files only the findings no sub-issue names, even if the work branch has moved since the ledger's sha, which the report notes. A ledger that can't be parsed stops the run; the loop never reviews again over it.
- **The tasks a review filed** are the ledger's, not the sub-issues that appeared while it ran.

## Considered options

- **The agent files, guided by a ledger comment it keeps itself.** Matching by finding id is exact, but recovery rests on each retry's agent noticing and following the resume steps.
- **The agent rolls back what it filed** (closing as not planned) before failing. Undoing takes more tracker calls, which can fail the same way.
- **The agent dedupes a fresh review against existing sub-issues by wording.** Fuzzy; it misses duplicates.
- **The findings file as the only record.** It lives on one machine, so a review half-filed on a box would be reviewed again from the operator's machine.

## Consequences

- An ejected review prompt must write the findings file; a skill that files issues itself, such as `review-and-task` as it stands, can't be called unchanged. The file's format is a contract the agent guide documents.
- `gh` compatibility (`--parent`, `--blocked-by` and their fallbacks) sits in smith's tracker code, not in the prompt.
