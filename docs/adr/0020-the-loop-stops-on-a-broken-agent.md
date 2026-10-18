# The loop stops on a broken agent instead of skipping

> Amends [ADR-0015](./0015-the-loop-marks-tasks-as-a-signal-not-a-claim.md), which had the loop skip any task left open after its last attempt, and [ADR-0016](./0016-the-loop-comments-on-a-task-it-skips.md), whose skip comment a run stopped this way never posts.

## Context

When the coding agent cannot run at all (an expired or revoked credential, a missing binary, a broken prompt), every launch fails. The loop counted each failure as an attempt and skipped each task once its attempts ran out, so one dead credential walked the whole spec: every task was handed to a human, labelled `smith:failed`, and given a comment that blamed the task for what was the machine's fault.

## Decision

**A task is skipped only when at least one of its attempts was not an agent failure. When every attempt at a task is an agent failure, the run stops as `agent broken` instead.**

- **An agent failure** is a run the agent CLI ends with a non-zero exit. A run killed by SIGKILL (usually one task running out of memory) or a clean exit that leaves the task open is a failed attempt but not an agent failure, since the agent itself ran.
- **An agent that cannot be started at all** stops the run as `agent broken` on the first launch, without counting an attempt: no retry can fix it, as with a failed worktree preparation.
- **The task the run stops on is left as the next run should find it**: its `smith:working` mark is removed, it is not handed to a human or labelled `smith:failed`, it gets no skip comment, and its task worktree is kept.
- **`agent broken` is its own ending**, recorded and posted on the spec like the others. The stop report names the task, the last agent failure's error, and the machine's hostname, and tells the operator to check that the agent runs there. It names no credential, so smith stays credential-agnostic.
- **Interactive runs are unchanged**: they make one attempt, never skip, and the operator sees the failure directly.

## Considered options

- **Stopping when agent failures in a row span two tasks.** Rejected: with the attempts at one task run back to back, the first task would already have been skipped and commented on by the time the second task's first run failed, unless its skip was held back in pending state. A single-task execution would also never trip.
- **Checking the agent's credential before the run.** Rejected in #297: `claude auth status` passes a revoked credential, and a real check spends a model request.
- **A configurable streak threshold.** Rejected: the rule rides on `MaxAttempts`, so there is no second number to tune.

## Consequences

- A task that makes the agent CLI itself fail on every attempt, for a reason of its own such as a prompt too long for the model, stops the run instead of being skipped. That fails loudly rather than silently, and the stop report names the task.
