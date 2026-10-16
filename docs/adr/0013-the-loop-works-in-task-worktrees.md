# The loop works each task in its own worktree, merged into a work branch

> Amends [ADR-0012](./0012-the-repo-file.md), which left the loop working in the repo it is invoked from.

## Context

The loop runs an execution's tasks one after another in the checkout it is invoked from, on whatever branch that checkout has, and never creates or switches a branch. After each closed task it pushes that branch, skipping the push when the branch is `origin`'s default, because running the loop on `main` put the agent's commits on local `main`.

Smith is heading for parallel tasks. Two agents cannot share one working tree, so the loop has to stop working in the operator's checkout. This decision makes that change while tasks still run one at a time, so the branch model is settled before parallel runs arrive.

## Decision

**Every execution has a work branch, and every task is worked in its own task worktree on its own task branch, cut from the work branch's tip and merged back into it.** There is no in-place mode: a second code path would make the parallel work harder and offers nothing a work branch cannot.

- **The work branch** is named `smith/<n>-<slug>` after the issue the execution is derived from (a spec, or later a standalone task, which share GitHub's number space), or exactly as `--branch` names it. An existing work branch is adopted: a local one is reused, one only on `origin` is created from origin's copy, and one that has diverged from origin's is refused. An adopted branch is compared with the tip of its base on `origin`, only read, and one the base has moved past gets a `smith: warning:` line before any agent runs; the run carries on. A missing one is cut from `--base`, which defaults to `origin`'s default branch. The default lookup finds the branch by number (`smith/<n>-*`), so renaming the issue does not strand it, and more than one match is an error. The loop refuses to start when the work branch is `origin`'s default branch or the base.
- **Task worktrees live under `<git-common-dir>/smith/worktrees/`.** That is `.git` in an ordinary clone and the bare `repo.git` on a box, so the operator's checkout is never dirtied and the box's session worktrees are never collided with.
- **Merging is fast-forward only** while tasks run one at a time, since nothing else moves the work branch. A task branch that cannot fast-forward stops the run. If a clean worktree has the work branch checked out, the loop merges there so that checkout follows; if it is dirty, the loop refuses before starting; otherwise it moves the ref with a guard that it has not moved since it was read.
- **A task is done when the tracker says it is closed**, as before. A failed attempt keeps its task worktree and branch, so the retry continues where it stopped, and a skipped task's are left for a human. A closed task whose worktree is dirty stops the run without merging, reopening or deleting anything. A closed task with no commits is fine.
- **Only the work branch is pushed**, after each merge, never forced. Task branches stay local and are removed with their worktrees once merged. `Origin.Push` loses its default-branch skip; the default-branch lookup moves to start-up, where it gives `--base` its default and fails before any agent runs.
- **The base branch is only read.** The loop never brings its later commits into the work branch, now or with parallel runs: integration with the base belongs to review.
- **The repo file and prompt are read from the invoking checkout**, as before.

Interactive runs take the same path, with the attached agent working in the task worktree.

## Considered options

- **Keep in-place work as an opt-out.** Rejected: two paths to keep correct, and the in-place path is the one parallel runs cannot use.
- **The branch the operator stands on is the work branch.** Rejected as the default because it ties the loop to the invoking checkout, which parallel runs and an unattended smith do not have. `--branch` keeps the operator's choice of name.
- **Task worktrees beside the checkout or under the workspace's `worktrees/`.** Rejected: the first dirties the operator's directory tree, the second collides with session worktrees on a box, and neither works the same for a normal clone and a bare repo.
- **Merge or rebase the base into the work branch during a run.** Rejected: conflicts would surface inside the loop, where nobody is watching.

## Consequences

- Docs designed with a spec reach the loop by being committed to the work branch first. For now the operator renames the branch to `smith/<n>-<slug>` after `/to-spec` and `/spec-to-tasks` give the number, and pushes it, and the loop adopts it locally or, later, on a box.
- An unattended smith that watches the tracker pairs a spec with its work branch by number. It needs a spec-level go signal applied last (a smith marker, perhaps with a short pickup delay), so that a missing branch always means "cut from the base" and never "not pushed yet".
- The box side is not built yet: an agent-driven session holding the work branch, with task worktrees beside it, is the shape this leaves room for.
- Opening a PR from the work branch stays out of the loop.
