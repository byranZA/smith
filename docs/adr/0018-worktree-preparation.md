# Worktree preparation — the repo says how, smith runs it in every worktree, and the loop recycles task worktrees

> Amends [ADR-0012](./0012-the-repo-file.md), which gave the repo file the loop's settings only, and [ADR-0013](./0013-the-loop-works-in-task-worktrees.md), which removed a task worktree once its branch was merged.

## Context

A repo knows two things about itself that smith never acts on: the toolchain its committed `mise.toml` pins, and the commands (`uv sync`, `npm ci`, `make tools-install`) that must run before anyone can run its checks. `workspace converge` installs only the versions smith's own generated configs pin, and `session start` creates the worktree and places files in it, nothing more.

`mise exec` installs a missing version on its own by default, so a committed `mise.toml` does resolve in the end. But it does so inside the tmux session, where a loop-driven session fails with nobody watching. Dependencies are never installed at all.

The loop makes this a cost question as well as a correctness one. Agents work in task worktrees, not the session's, and ADR-0013 cut a fresh one for every task. A Python repo whose install takes five to ten minutes would pay that once per task.

## Decision

**Every worktree smith creates for a driver to work in is prepared: the toolchain the repo's own mise config pins is installed, then the repo's prepare command runs under `mise exec` in the worktree.** A human session's worktree is prepared on every `session start` that launches it, new or restarted, and never when the session is already live. A task worktree is prepared before every agent attempt. A loop-driven session's own worktree, where only smith runs, is not prepared.

- **The repo declares how; smith knows no language.** The prepare command is one string, `prepare:` in the repo file, run with `sh -c`. It is read from the worktree being prepared, so a branch that changes how it prepares is prepared its own way. A repo that declares none still gets its toolchain installed. The command must be safe to re-run, because recycling and restarts re-run it on a tree it already prepared.
- **It runs without asking.** A committed prepare command is code from the repo, but so is `make check`, which the agent already runs. A repo the operator declared is trusted to the same degree.
- **The loop recycles task worktrees.** Once a task branch is merged, the branch is removed and the next task's branch is switched into the same worktree. What the repo ignores (a `.venv`, `node_modules`) carries over, so preparation runs incrementally rather than from scratch: one full preparation per execution, and later one per parallel slot, not one per task. A skipped task's worktree is still left for a human, so the task after it cuts a fresh one. When the run ends `complete` the worktree is removed; on any other outcome it is kept for the next run of the same execution.
- **Failure depends on the driver.** A failed preparation in a human session is a `smith: warning:` naming the step and its exit status, and the session launches anyway, because the human is the one who fixes it. In the loop it stops the run with outcome `failed`, and the stop report names preparation, the task and the worktree. It does not count as a failed attempt, since retrying the agent cannot fix a broken toolchain or prepare command.
- **Output goes where someone is watching.** `session start` prepares in the foreground before the session exists, streamed back over the relay like the loop's preflight ([ADR-0017](./0017-the-loop-starts-its-own-session-on-a-box.md)). The loop prepares within its own output, visible to anyone attached.
- **Locally too.** The loop prepares task worktrees on the operator's machine as on a box. The toolchain step runs only where mise is on PATH, which a box guarantees, and the prepare command runs in the operator's own environment otherwise.
- **No timeout.** A slow first install is legitimate. Ctrl-C, `session stop` and SIGHUP already end it cleanly.

**System packages stay in the blueprint.** `apt` changes the whole box, not one worktree: two repos would get each other's packages, nothing would remove them, and a repo file from any clone could install software as root. A repo that needs `make` says so by failing its prepare command clearly, and the operator adds it to the blueprint's `packages`.

## Considered options

- **Opt-in toolchain install.** Rejected: installing what a committed `mise.toml` pins is what "nearer wins" already promises, and no repo wants the alternative.
- **Leaving the toolchain to `mise exec`'s auto-install.** Rejected: the install happens inside the session, so a failure surfaces where nobody may be watching.
- **The prepare command under the blueprint's `repos[]` entry.** Rejected: how a checkout is prepared is the same for everyone running the repo, which is the repo file's job by ADR-0012.
- **Detecting the ecosystem from lockfiles.** Rejected: smith would have to learn every ecosystem and would guess wrong in a monorepo, when the repo can say what it means.
- **Packages in the repo file, installed with sudo.** Rejected for the reasons above. Checking and naming missing packages without installing them was also considered, and left out until a repo needs more than its prepare command failing.
- **Copying a prepared `.venv` or `node_modules` into a new worktree.** Rejected: Python virtualenvs bake absolute paths into `pyvenv.cfg` and script shebangs, and the copy would make smith language-aware.
- **One environment shared across worktrees.** Rejected: it breaks as soon as two branches disagree about dependencies. A repo can still do it in its own prepare command, keyed by its lockfile.
- **Failing `session start` on a failed preparation.** Rejected: it locks the human out of the shell they need to repair the command.

## Consequences

- Ignored files left by one task are present for the next. Tracked and untracked changes are not, because the loop already refuses a dirty tree after a closed task.
- Package managers with a shared cache (uv, pnpm, Go's module cache) make the full preparations that remain cheap; that is the repo's choice of tooling, not smith's.
- The repo file gains its first key that is not about the loop alone: a human session reads it too.
