# The loop

The **loop** works a spec's tasks, or a single task, with a coding agent, one task per agent run. It asks the tracker which task is next, hands exactly that task to an agent, and reads the tracker again afterwards to see whether the agent closed it. The loop decides when it is done, not the agent.

```text
smith loop list <issue>
smith loop run  <issue> [--agent <name>] [--model <name>] [--effort low|medium|high]
                        [--max-iterations <n>] [--max-attempts <n>] [--interactive] [--no-push]
                        [--branch <name>] [--base <branch>]
smith loop prompt
smith repo init [--agent <name>] [--model <name>] [--effort low|medium|high]
```

Every verb runs in the repo it is invoked from, but no agent works in your checkout: `run` works each task in a worktree of its own, off the work branch (see [The work branch](#the-work-branch)), and leaves the branch you stand on and its uncommitted changes alone. It never commits itself. The prompt asks the agent to commit, so a failed attempt's changes are still in its task worktree for the next attempt. After each task the agent closes, the loop brings its commits onto the work branch and pushes that to `origin` (see [Pushing](#pushing)), so closed work never exists on one machine only.

## What the loop reads from GitHub

The tracker is GitHub Issues, reached through `gh`, so `gh` must be installed and authenticated.

- **The issue** the loop is given is named as `42`, `#42` or its issue URL, and is a spec or a single task. A URL for an issue in a different repo from the one `gh` sees in the working directory is refused.
- **A spec** is an issue labelled `spec`. Any other issue is a task, worked alone as a run of one task. A **child task**, one with a parent, runs on its immediate parent's work branch, and no sibling task is worked. A **standalone task**, with no parent, runs on a work branch of its own.
- **Its tasks** are its sub-issues. The ones its `## Tasks` checklist lists come first, in that order, then any other sub-issue in the order GitHub holds them.
- **Who a task is for** comes from its labels: `ready-for-agent` tasks go to the agent, and `ready-for-human` tasks are never handed out. A task with both labels is kept for the human. A task with neither is not ready yet, and a sub-issue labelled `spec` is a nested spec, never a task.
- **Blockers** are a task's GitHub "blocked by" links, plus any issue named in its `## Blocked by` section. A task is **available** when it is open, for an agent, and every blocker is closed.

A task named on its own must be available. Otherwise `run` launches no agent, sets up no work branch or worktree, and exits 1 naming why: `#N is closed`, `#N is meant for a human`, `#N is not labelled "ready-for-agent"` or `#N is blocked by #M`. There is no override. `list` still shows the task under where it stands, such as `blocked: #N (by #M)`, and exits 0.

The loop hands out the first available task in the spec's order. It reads the tracker again before every choice, so a task filed or unblocked mid-run is seen.

## `smith loop list`

Shows the settings a run would use, the next task, and what remains, without running anything:

```text
agent:  claude (repo file)
model:  opus (repo file)
effort: high (repo file)
push:   on (built-in default)
spec #244 Spec: the loop — smith loop and the repo file
next: #253 Loop: smith runs its own loop — .smith/ config, prompt and docs
  blocked: #254 (by #253)
  waiting on a human: #255
  closed: #245, #246, #247
```

Each setting names where its value came from. A kind of task with nothing in it is left out.

## `smith loop run`

Prints the same settings block as `list`, then works the spec or task unattended until no task is available. As it goes, it writes `smith: handing #N <title> to the agent in <worktree>` to stderr before each agent run, and `smith: the agent on #N ended with a failure: …` when one fails. At the end it reports why it stopped, then names the work branch (`work branch: smith/N-<slug>`):

- **`spec #N complete`**, or **`task #N complete`** for a standalone task: every task is closed. Exit 0.
- **No task is available for an agent**: what remains is blocked, for a human, or not ready. Exit 1.
- **The agent left a task open on every attempt**: see attempts below. Exit 1.
- **Reached the cap of N agent runs**: see iterations below. Exit 1.

Two limits bound a run so it can be left alone:

- **`--max-attempts`** (default 2): a task the agent leaves open is handed out again until it has had this many runs, then skipped for the rest of the run. A skipped task stays open on GitHub, usually with the agent's WIP note on it, and its task worktree and task branch are left for you: the report names the worktree (`#N left in <worktree>`).
- **`--max-iterations`** (default 10): the most agent runs the whole loop makes.

An agent that exits with a failure is reported and the loop carries on: whether the task is done is the tracker's call, not the exit code's. Ctrl-C stops the running agent and starts no further task.

### The work branch

Each run's work lands on its **work branch**, `smith/<n>-<slug>`: the number of the spec, of a child task's immediate parent, or of the standalone task, then its title lower-cased, with each run of other characters made one hyphen, without a leading `Spec:`, and cut at a word to at most 40 characters. Spec #430 titled "Spec: Loop works in task worktrees" works on `smith/430-loop-works-in-task-worktrees`. `--branch` names the work branch instead, used exactly as written, and no branch is then looked up by number. An existing work branch is adopted, found by number (`smith/<n>-*`) so an issue renamed since keeps its branch: a local one is reused as it is, and one only on `origin` is created locally from `origin`'s copy. That is how a branch holding the spec's docs reaches the loop. It is also how runs share a branch: `loop run 82`, for a child of spec #74, adopts `smith/74-<slug>` or cuts it, and a later `loop run 74` adopts it again and works only the tasks still open. The run stops before any agent runs when more than one branch matches the number, naming each, or when the local and `origin` copies have diverged, naming the branch. When no branch matches, the loop cuts one from the tip of the branch `--base` names on `origin`. Without `--base`, it cuts it from the tip of `origin`'s default branch, which it asks `origin` for with `git ls-remote --symref origin HEAD`, so it works in a bare clone, which has no `origin/HEAD`. If `origin` names no default branch and no `--base` is given, the run stops before any agent runs, saying how to give `origin` one. A work branch checked out in a worktree with uncommitted changes, such as your own checkout, also stops the run before any agent runs, naming that worktree. The work branch may be neither `origin`'s default branch nor the base: a run whose work branch is either stops before any agent runs, and nothing is pushed.

Each task is worked in a **task worktree** under `<git-common-dir>/smith/worktrees/<task>` (`.git/smith/worktrees/` in an ordinary clone), on a **task branch**, `smith/tasks/<work-branch>/<task>`, cut from the work branch's tip. Anything already at a task worktree's path that is not this repo's worktree on that task's branch, such as one left by a run on another work branch, stops the run and is left in place. A task the agent leaves open keeps its worktree, so the next attempt continues where the last stopped. Once the tracker says a task is closed, the loop fast-forwards the work branch to the task branch, with `git merge --ff-only` in the worktree that has the work branch checked out if one does, so a checkout of the work branch follows the loop, and removes the task worktree and task branch. A task closed without commits lands as a no-op, its worktree and branch still removed. A closed task whose worktree holds uncommitted changes stops the run naming the worktree, which is kept. So does a work-branch checkout that picked up uncommitted changes during the run, naming that checkout. A task whose branch the work branch cannot fast-forward to stops the run naming the work branch. Either way the work branch is unchanged, the task stays closed, and nothing is pushed. The [worktrees ADR](./adr/0013-the-loop-works-in-task-worktrees.md) has the reasoning.

### Pushing

After each task lands, and before the loop chooses the next, it pushes the work branch to `origin` under the same name, setting that as the branch's upstream so a later bare `git push` works. It writes `smith: pushed #N on <branch> to origin` to stderr. The agent never pushes; the loop does. Task branches are never pushed. The loop never forces a push, and a task the agent closed without committing pushes nothing new, which is fine.

- **A failed push stops the run** (auth, a rejected push, the network), exiting non-zero and naming the task and git's error. The task stays closed, because its work is committed and landed, and no further task starts, so unpushed work cannot pile up. Fix the cause and push by hand, or run the loop again.

Pushing is on unless the repo file sets `push: false`; `--no-push` turns it off for one run, and the work branch stays local. `--interactive` lands and pushes the same way once its task is closed.

### `--interactive`

Hands the next available task to one agent run attached to your terminal, in the task's worktree like any other run, then stops. The prompt gains a note asking the agent to check its plan with you, ask rather than guess, and show you the result before it commits and closes the task. Approvals are the agent's own, so you answer them as you would in any session. The limits play no part. It reports `task #N closed` and exits 0, or `task #N still open` and exits 1. With no task available it runs no agent, prints the same stop report as `run`, and exits 0 only if the spec is complete.

Use it for the first run on a new repo or prompt, or for a task you want to steer.

## Agents

An **agent adapter** is smith's built-in knowledge of one coding-agent CLI. The CLI must be on your `PATH`; `run` refuses before starting if it is not.

| agent | unattended run | effort becomes |
| --- | --- | --- |
| `claude` (default) | `claude --permission-mode auto --print <prompt>` | `--effort` |
| `codex` | `codex exec --yolo <prompt>` | `--config model_reasoning_effort=…` |
| `pi` | `pi --print <prompt>` | `--thinking` |

An unattended `claude` run also sets `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`, so the run ends when the agent's work does.

> **An unattended run asks you nothing.** `codex --yolo` bypasses both its approvals and its sandbox. Run the unattended loop where an agent with your shell is acceptable: a box, a container, or a machine you would trust it on.

The model is passed to the agent verbatim. Effort is on smith's own scale (`low`, `medium`, `high`) and each adapter translates it. Left unset, either one is the agent's own default.

## The repo file

`.smith/repo.yaml`, at the git repo's root, holds the settings everyone running the loop on the repo shares. Commit it. It is optional; without it every setting is the built-in default.

```yaml
agent: claude
model: opus
effort: high
push: true
```

`smith repo init` writes a commented starter, filling in any value you give as a flag, and reports `created <path>`. It never overwrites an existing file: it reports `left alone <path>` instead. Outside a git repo it refuses: `must be run inside a git repo`.

Each setting resolves on its own, most specific first:

1. A flag on `smith loop run`, for that run only.
2. The repo file.
3. The built-in default: `claude`, the agent's own model and effort, and push on.

The file is validated strictly: an unknown key is refused with its line. An unknown agent or an effort off the scale is refused when the loop resolves it, naming where the value came from (`agent (repo file): …`); a flag that overrides the bad value hides it. `repo init` writes the values you give it without checking them. A repo whose `.smith/` is your config home (a home directory kept in git, say) is refused, so your own config and a repo's never share a directory. The [repo file ADR](./adr/0012-the-repo-file.md) has the reasoning.

## The loop prompt

The loop hands each agent run the **loop prompt**. smith ships a built-in one:

```sh
smith loop prompt
```

It is deliberately generic. It tells the agent how to work one task, read the task and its spec, and close the task or leave a WIP note. For anything specific to the repo, it sends the agent to the repo's own agent instructions (`AGENTS.md`, `CLAUDE.md` or similar): its checks, commit convention, design rules and the paths to leave alone. Put repo-specific rules there rather than in the prompt. They then reach every agent working in the repo, in the loop or not, and the prompt can follow smith's default.

To change the prompt itself, eject it:

```sh
smith loop prompt > .smith/prompt.md
```

Once `.smith/prompt.md` exists, the loop uses it instead of the built-in. smith never overwrites it, so the repo owns it from then on. A repo that never ejects follows smith's latest prompt on every upgrade. One that has ejected catches up by diffing:

```sh
diff <(smith loop prompt) .smith/prompt.md
```

The prompt takes these placeholders, filled in for each task:

| placeholder | becomes |
| --- | --- |
| `{{TASK_NUMBER}}` | the task's issue number |
| `{{TASK_TITLE}}` | the task's title |

A prompt holding any other `{{…}}` is refused, naming it.
