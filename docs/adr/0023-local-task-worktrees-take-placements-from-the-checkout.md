# Off a box, task worktrees take the blueprint's placements from the checkout

> Amends [ADR-0019](./0019-worktree-preparation.md), whose "Locally too" left placements to the box.

## Context

A repo often needs gitignored files to work in, such as `.env`. A blueprint declares them as repo-scoped placements, and on a box the loop places them into every task worktree from the bytes `machine setup` staged ([#629](https://github.com/byranZA/smith/issues/629)). An ordinary clone on the operator's machine has no staged tree and no blueprint tied to the run, so its task worktrees got nothing.

The workaround was a copy in `prepare:`, such as `cp ../../.env .env`. It puts a path to one operator's checkout into a committed file and fails without naming what went wrong when the source moves. [ADR-0021](./0021-task-worktrees-of-a-non-bare-repo-live-in-the-state-home.md) broke it outright: an ordinary clone's task worktrees live in the state home, so no relative path reaches the checkout.

## Decision

**The blueprint's `repos[].placements` is the one declaration of a repo's files, on a box and off one. Off a box, the loop places the same paths into each task worktree, with the bytes copied from the checkout the loop was started in.** To the operator it is one mechanism: the blueprint says which files a repo's worktrees get, and smith supplies them wherever the loop runs. The repo file does not change.

- **Off a box means nothing is staged**, the test the loop already uses. A Docker local box has a staged tree, so it is a box.
- **The checkout is the top level of the worktree the loop runs in.** A linked worktree of the clone is its own source, and a subdirectory resolves to its top level.
- **The blueprint is found by its repo, not chosen.** Every blueprint in the config home is read for its repos' URLs and placement paths, and a repo entry matches when its `url` names the same repo as the checkout's `origin`. Both URLs are normalised to host and path, lowercased, without scheme, user or `.git`, so an SSH and an HTTPS URL of one repo match. The name is not a fallback, because two unrelated repos can share one.
- **Several matches combine.** Only the paths matter when the bytes come from the checkout, so the placements of every matching entry are joined, and where two disagree on a path's mode, `converge` wins. No match places nothing and says nothing, since most repos are in no blueprint. A blueprint that cannot be read is a `smith: warning:` naming it, and the run goes on without it. No source reference is resolved.
- **The placement rule is the box's.** Placement happens before every attempt and before preparation. A `once` file is written only when absent; a `converge` file is replaced by a whole-file copy, never a symlink, which would let an agent write through to the checkout. The blueprint's `perms` apply, as on a box. On Windows only the read-only bit takes effect, and a read-only placement cannot be replaced by a later `converge`; that is documented, not worked around.
- **The checks are the box's, plus the source.** Before any task is marked, the loop stops when a destination is not gitignored in the task worktree, and also when a source is missing from the checkout or not gitignored there. A tracked source is already in the worktree, so copying it would carry uncommitted edits into the task. A missing source stops the run with an error naming the path, the checkout and the blueprint that declares it.

## Considered options

- **A `placements:` list in the repo file.** The paths would sit with the repo, and every teammate would get the same list. Rejected because the box already reads the blueprint, so the operator would declare one file in two places.
- **Resolving the blueprint's source references on the operator's machine, as `machine setup` does.** Rejected: a local run has no blueprint tied to it, so this needs a `--blueprint` flag or a preference, and the checkout already holds the file the operator uses there.
- **Leaving it to `prepare:` with a variable naming the checkout.** The cheapest. Rejected because every repo would write its own copy, no gitignore check would guard it, and a missing source would surface as a prepare failure.
- **Skipping a missing source.** Rejected: running without a file the blueprint says the repo needs is the quiet failure this decision exists to remove.

## Consequences

- A blueprint that places a file the operator does not keep in their checkout stops every local loop run of that repo until the file exists.
- A local run's placements depend on the config home, so two operators with different blueprints for one repo get different files. The box already behaves this way.
- Per-worktree identity in the environment ([#546](https://github.com/byranZA/smith/issues/546)) is a separate mechanism, not a placement.
