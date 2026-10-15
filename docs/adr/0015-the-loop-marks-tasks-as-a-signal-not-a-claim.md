# The loop marks tasks on the tracker as a signal, not a claim

> Amends [ADR-0012](./0012-the-repo-file.md), which had the loop only read the tracker.

## Context

Nothing on the tracker shows that the loop is working a task or that it gave up on one, so an untouched `ready-for-agent` task looks the same as one an agent is halfway through. Showing it means the loop's first write to the tracker. Parallel tasks are coming ([ADR-0013](./0013-the-loop-works-in-task-worktrees.md)), which raises the question of whether a "being worked" label should also stop a task being handed out twice.

## Decision

**The loop labels the tasks it works, and the labels are a signal for people: the loop never reads them to choose a task.** Parallel tasks run inside one loop process, which already knows what it has handed out, so it needs no claim on the tracker. One process works a spec at a time.

- **`smith:working`** is on a task only while an agent runs on it. The loop adds it when it hands the task out and removes it when the agent exits, whether the task closed, stayed open for a retry, or was skipped. An interrupted run removes it as best it can.
- **A run removes `smith:working` from every task of its spec when it starts**, clearing what a killed run or a lost machine left behind.
- **A skipped task goes to a human**: the loop moves it from `ready-for-agent` to `ready-for-human` and labels it `smith:failed`. A human who moves it back to `ready-for-agent` sends it to the loop again, and the loop removes `smith:failed` when it next hands the task out.
- **Interactive runs** add and remove `smith:working`, but never apply `smith:failed`: they make one attempt and never skip.
- **A failed label write is a warning, and the run carries on.** A wrong label misleads but loses no work, unlike a failed push.
- **The labels are hardcoded**, like the loop's others, and the loop creates one that is missing from the repo the first time it needs it. Writes go through the same tracker seam as reads.

## Considered options

- **A claim the loop honours when picking a task.** Rejected: one process already holds its own claims in memory. Across processes, a label is no lock anyway: GitHub has no compare-and-swap on labels, so two processes can both add one and both start, and telling a live claim from a dead one needs an owner identity and a lease.
- **Clearing stale labels only when the task is next handed out.** Rejected: a task nobody hands out again would read as worked forever.
- **Only labelling a skipped task, leaving it `ready-for-agent`.** Rejected: the next run would try it again, and nothing puts it in front of a person.

## Consequences

- Working a spec from more than one process needs a design of its own: which process owns an execution, or a real claim with an owner and a lease. Two decisions here hold only while one process works a spec and must be revisited then: the start-up sweep, which would strip a live process's label, and warning on a failed write, which a claim could not afford.
- Nothing yet refuses a second process on the same spec; until something does, running two is the operator's mistake to avoid.
