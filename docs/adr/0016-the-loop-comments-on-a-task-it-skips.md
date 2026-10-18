# The loop comments on a task it skips

> Amends [ADR-0015](./0015-the-loop-marks-tasks-as-a-signal-not-a-claim.md), which had the loop write only labels to the tracker.
>
> **Amended by [ADR-0017](./0017-the-loop-starts-its-own-session-on-a-box.md).** The loop also posts its stop report as a comment on the spec.
>
> **Amended by [ADR-0020](./0020-the-loop-stops-on-a-broken-agent.md).** A run that stops as `agent broken` skips no task, so posts no skip comment.

## Context

A skipped task is labelled `smith:failed` and handed to a human, but its task worktree and task branch stay on the machine the loop ran on ([ADR-0013](./0013-the-loop-works-in-task-worktrees.md)). Only the operator's terminal learns where they are. A person picking the task up from the issue can see that it failed, not where the leftover work is or whether the agent got anywhere.

## Decision

**When the loop skips a task, it posts one comment on the task's issue saying where the work was left.** It is posted as the task is handed to a human, beside the `smith:failed` label change, and says it comes from smith. It names:

- **Machine:** the hostname of the machine the loop ran on, locally and on a box alike.
- **Task worktree:** its absolute path on that machine.
- **Task branch:** its name.
- **Attempts:** the agent runs the task got in this run.
- **Progress:** the commits the task branch has beyond the work branch, so "nothing committed" reads apart from "some work committed".

The loop writes the comment's wording; the tracker only posts it.

- **A failed comment write is a warning, and the run carries on**, as a failed label write is. A hostname or commit count the loop cannot read is marked unknown in the comment, with a warning, and the comment still goes out.
- **Nothing else is commented on.** Starting, stopping and closing a task stay label-only, an interactive run never skips so never comments, and a later success leaves the old comment as it is.
- **Nothing reads the comment back.** Like the labels, it is a signal for people.

## Considered options

- **Carrying the note on `Fail`.** Rejected: a comment is a separate write that can fail on its own, and keeping it apart leaves `Fail`'s label change as it was.
- **Naming the machine by its smith box name or tailnet name.** Rejected for now: the hostname is known everywhere the loop runs with no lookup.
- **Pushing the task branch so the work is reachable off the machine.** Rejected: ADR-0013 keeps task branches local.

## Consequences

- Each skip adds a comment, so a task skipped on several runs collects one per run; nothing edits or deletes an earlier one.
- The comment is the loop's first tracker write that is not a label, and goes through the same tracker seam.
