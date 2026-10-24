# Task worktrees of a non-bare repo live in the state home

> Amends [ADR-0013](./0013-the-loop-works-in-task-worktrees.md), which put every task worktree under `<git-common-dir>/smith/worktrees/`.

## Context

ADR-0013 chose the git common directory so that a box's bare `repo.git` and an ordinary clone's `.git` would work the same way. In an ordinary clone that puts task worktrees at `.git/smith/worktrees/…`, inside the operator's checkout, and common tooling breaks there in ways that don't point back to smith ([#571](https://github.com/byranZA/smith/issues/571)):

- **Tools that skip VCS directories see an empty project.** Jest's haste map ignores every path with a directory named exactly `.git`, so `npm test` in a task worktree finds no tests and exits 1. Metro and watchman ignore `.git` too.
- **Upward config lookup reaches the operator's checkout.** ESLint, Prettier, Babel and editorconfig walk up from the task worktree and load the checkout's config, and sometimes its `node_modules`.

The agent treats a failing check as real, so it leaves every task open. A box's bare repo has neither problem: `repo.git` is not named `.git`, and no checkout surrounds it.

## Decision

**A non-bare repo's task worktrees live in the state home, at `<state home>/worktrees/<repo>-<hash>/<work-branch>/`. A bare repo's stay under `<git-common-dir>/smith/worktrees/<work-branch>/`.**

- **Bareness decides, not where smith runs.** The rule reads the repo's `core.bare`, so a box behaves as before, foreground runs in a box session's worktree included. An ordinary clone hand-made on a box moves too.
- **The state home** is `$XDG_STATE_HOME/smith/` when that variable is an absolute path, else `~/.local/state/smith/`, on every OS. Task worktrees hold work in progress: they are not config, so they don't belong in the config home, and they can't be rebuilt, so they don't belong in a cache.
- **`<repo>-<hash>`** is the checkout's directory name and a short hash of the absolute git common directory. The name keeps the path readable, and the hash keeps two clones of one repo apart.
- **The location is not configurable.** One rule covers both reasons, and nothing asks for another location.
- **Nothing is migrated.** A failed or skipped task's worktree at the old location is still found by its task branch. A recycled worktree a stopped run left there is not reused, and the release notes say to remove it with `git worktree remove`.

## Considered options

- **Keep one location for both kinds of repo.** Rejected: ADR-0013's reason, that the two work the same, doesn't hold when tools treat a `.git` path segment and an enclosing checkout specially.
- **The config home, `~/.smith/worktrees/`.** Rejected: the config home holds config and a rebuildable cache, and the operator may version it.
- **Beside the checkout.** Rejected, as in ADR-0013: it dirties the operator's directory tree.
- **A configurable location.** Rejected until someone needs it: a settings field adds surface to a path that is cheap to change.

## Consequences

- A task worktree outlives a checkout that is deleted or moved: git loses track of it, and smith does not clean it up.
- A run whose `$XDG_STATE_HOME` differs from the previous run's looks under a different root. A recycled worktree is then left behind, as after an upgrade; no work is lost, because a failed or skipped task's worktree is found by branch.
