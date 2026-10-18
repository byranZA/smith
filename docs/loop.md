# The loop

The **loop** works a spec's tasks, or a single task, with a coding agent, one task per agent run. It asks the tracker which task is next, hands exactly that task to an agent, and reads the tracker again afterwards to see whether the agent closed it. The loop decides when it is done, not the agent.

```text
smith loop list <issue>
smith loop run  [<box>] <issue> [--agent <name>] [--model <name>] [--effort low|medium|high]
                        [--max-iterations <n>] [--max-attempts <n>] [--interactive] [--no-push]
                        [--branch <name>] [--base <branch>] [--repo <name>]
smith loop prompt
smith repo init [--agent <name>] [--model <name>] [--effort low|medium|high]
```

Every verb runs in the repo it is invoked from, but no agent works in your checkout: `run` works each task in a worktree of its own, off the work branch (see [The work branch](#the-work-branch)), and leaves the branch you stand on and its uncommitted changes alone. It never commits itself. The prompt asks the agent to commit, so a failed attempt's changes are still in its task worktree for the next attempt. After each task the agent closes, the loop brings its commits onto the work branch and pushes that to `origin` (see [Pushing](#pushing)), so closed work never exists on one machine only.

## What the loop reads from GitHub

The tracker is GitHub Issues, reached through `gh`, so `gh` must be installed and authenticated.

- **The issue** the loop is given is named as `42`, `#42`, `owner/name#42` or its issue URL, and is a spec or a single task. A qualified reference or URL for an issue in a different repo from the one `gh` sees in the working directory is refused.
- **A spec** is an issue with sub-issues, whatever its labels, or one labelled `spec`, which marks a spec not yet split into tasks. An issue with sub-issues is a spec even when it is labelled `ready-for-agent`, and is never handed to an agent itself. Any other issue is a task, worked alone as a run of one task. A **child task**, one with a parent, runs on its immediate parent's work branch, and no sibling task is worked. A **standalone task**, with no parent, runs on a work branch of its own.
- **Its tasks** are its sub-issues. The ones its `## Tasks` checklist lists come first, in that order, then any other sub-issue in the order GitHub holds them.
- **Who a task is for** comes from its labels: `ready-for-agent` tasks go to the agent, and `ready-for-human` tasks are never handed out. A task with both labels is kept for the human. A task with neither is not ready yet, and a sub-issue that is itself a spec, labelled `spec` or with sub-issues of its own, is a nested spec, never a task. These are the default names of the **triage labels**: a repo can name its own (see [Triage labels](#triage-labels)), and every message names the labels the repo uses.
- **Blockers** are a task's GitHub "blocked by" links, plus any issue named in its `## Blocked by` section. A task is **available** when it is open, for an agent, and every blocker is closed.

A task named on its own must be available. Otherwise `run` launches no agent, sets up no work branch or worktree, and exits 1 naming why: `#N is closed`, `#N is meant for a human`, `#N is not labelled "ready-for-agent"` or `#N is blocked by #M`. There is no override. `list` still shows the task under where it stands, such as `blocked: #N (by #M)`, and exits 0.

A spec with no tasks yet is never handed to an agent: `run` sets up no work branch or worktree and exits 1 with `#N has no tasks`.

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

However the run stops, the loop also posts its stop report as a comment from smith on the issue it was given: the spec, or the task for a run of one task. The comment names the run's **outcome**, then carries the report and its `work branch:` line, so you can read how a run ended from anywhere, and GitHub notifies whoever follows the issue ([ADR-0017](./adr/0017-the-loop-starts-its-own-session-on-a-box.md)). The outcome is one of:

- **`complete`**: every task is closed.
- **`waiting on human`**: no task is available for an agent.
- **`attempts exhausted`**: the agent left a task open on every attempt.
- **`cap reached`**: the run reached its cap of agent runs.
- **`stopped`**: you interrupted the run, such as with Ctrl-C. The report says `interrupted`.
- **`failed`**: the run stopped on an error, such as a failed push. The report names the error.

Before every agent attempt, the first and each retry, the loop [prepares](./sessions.md#worktree-preparation) the task worktree, on your machine as on a box: it installs the toolchain the repo's mise config pins, then runs the worktree's [prepare command](#the-prepare-command), streaming both to stderr. A failed preparation launches no agent and stops the run with the outcome `failed`, its report naming preparation, the task and the task worktree (`prepare the worktree of #N in <worktree>: …`). Another attempt cannot fix it, so it counts as no attempt: the task is not labelled `smith:failed`, skipped or commented on. A loop-driven session's own worktree is not prepared, since only smith runs there.

A stop report that fails to post is a `smith: warning: …` line, and changes neither the outcome nor the exit status. A run whose issue cannot be read at all posts nothing. `--interactive` posts no stop report.

Two limits bound a run so it can be left alone:

- **`--max-attempts`** (default 2): a task the agent leaves open is handed out again until it has had this many runs, then skipped for the rest of the run. A skipped task stays open on GitHub, usually with the agent's WIP note on it, and is handed to you: the loop moves it from `ready-for-agent` to `ready-for-human` and labels it `smith:failed`. Moving it back from `ready-for-human` to `ready-for-agent` sends it back to the loop, which removes `smith:failed` when it next hands the task out. Its task worktree and task branch are left for you: the report names the worktree (`#N left in <worktree>`), and the loop comments on the task's issue with the machine's hostname, the worktree's path, the task branch, the attempts it made in this run, and how many commits the task branch has beyond the work branch ([ADR-0016](./adr/0016-the-loop-comments-on-a-task-it-skips.md)). A hostname or commit count it cannot read shows as unknown, and a comment that fails to post is a `smith: warning: …` line, as a failed label write is.
- **`--max-iterations`** (default 10): the most agent runs the whole loop makes.

An agent that exits with a failure is reported and the loop carries on: whether the task is done is the tracker's call, not the exit code's. Ctrl-C stops the running agent and starts no further task.

While an agent runs on a task, the loop labels the task `smith:working` on GitHub, and removes the label after every attempt, whether the agent closed the task, left it open or used its last attempt, when you interrupt the run, and when the tracker cannot be read after the agent exits, so you can see on the tracker which task is being worked. A run that is killed, or whose machine is lost, can leave the label behind, so an unattended run first removes `smith:working` from every task it is about to work, the spec's or the one task named, and from no other issue. The labels are signals for people: the loop never reads them to choose a task ([ADR-0015](./adr/0015-the-loop-marks-tasks-as-a-signal-not-a-claim.md)). A repo without one of these labels gets it created the first time it is needed. A label write that fails costs no work, so it is a `smith: warning: …` line on stderr naming the task, and the run carries on. `--interactive` marks its task the same way, but never hands a task to a human: a task it leaves open stays `ready-for-agent`.

### The work branch

Each run's work lands on its **work branch**, `smith/<n>-<slug>`: the number of the spec, of a child task's immediate parent, or of the standalone task, then its title lower-cased, with each run of other characters made one hyphen, without a leading `Spec:`, and cut at a word to at most 40 characters. Spec #430 titled "Spec: Loop works in task worktrees" works on `smith/430-loop-works-in-task-worktrees`. `--branch` names the work branch instead, used exactly as written, and no branch is then looked up by number. An existing work branch is adopted, found by number (`smith/<n>-*`) so an issue renamed since keeps its branch: a local one is reused as it is, and one only on `origin` is created locally from `origin`'s copy. An adopted branch is then compared with the tip of its base on `origin`, the branch `--base` names or `origin`'s default branch: when the base has commits the branch lacks, the run prints a `smith: warning: …` line on stderr before any agent runs, naming the branch, the base and how many commits behind it is, with the fix `merge or rebase origin/<base> into it`, and carries on. The loop never brings those commits in itself. A comparison that cannot run, such as a base `origin` does not have, is a `smith: warning: …` line saying why, and the run carries on too. A freshly cut branch is level with its base, and with no base at all there is nothing to compare, so neither is checked. That is how a branch holding the spec's docs reaches the loop. It is also how runs share a branch: `loop run 82`, for a child of spec #74, adopts `smith/74-<slug>` or cuts it, and a later `loop run 74` adopts it again and works only the tasks still open. The run stops before any agent runs when more than one branch matches the number, naming each, or when the local and `origin` copies have diverged, naming the branch. When no branch matches, the loop cuts one from the tip of the branch `--base` names on `origin`. Without `--base`, it cuts it from the tip of `origin`'s default branch, which it asks `origin` for with `git ls-remote --symref origin HEAD`, so it works in a bare clone, which has no `origin/HEAD`. If `origin` names no default branch and no `--base` is given, the run stops before any agent runs, saying how to give `origin` one. A work branch checked out in a worktree with uncommitted changes, such as your own checkout, also stops the run before any agent runs, naming that worktree. The work branch may be neither `origin`'s default branch nor the base: a run whose work branch is either stops before any agent runs, and nothing is pushed.

Each task is worked in a **task worktree** under `<git-common-dir>/smith/worktrees/<work-branch>/` (`.git/smith/worktrees/` in an ordinary clone), its directories nested along the work branch's `/` so no two work branches share one, on a **task branch**, `smith/tasks/<work-branch>/<task>`, cut from the work branch's tip. A task worktree is **recycled**: once a task lands, the next task's branch is switched into the same worktree, so what the repo ignores (a `.venv`, `node_modules`) carries over and preparation runs incrementally. A new worktree is cut only when there is none to recycle, at `<work-branch>/<task>` for the task it is cut for. Anything already at that path, such as a directory git does not know or a worktree on another branch, stops the run and is left in place. A task the agent leaves open keeps its worktree and branch, so the next attempt continues where the last stopped. A skipped task's are left for a human, so the task after it gets a new worktree. A run that ends `complete` removes the recycled worktree, with `git worktree remove`, so one holding changes is kept with a `smith: warning: …` line. A run that ends any other way keeps it, and the next run of the same execution works its first task in it. Once the tracker says a task is closed, the loop fast-forwards the work branch to the task branch, with `git merge --ff-only` in the worktree that has the work branch checked out if one does, so a checkout of the work branch follows the loop, then detaches the task worktree at the landed commit and removes the task branch. A task closed without commits lands as a no-op, its branch still removed. A closed task whose worktree holds uncommitted changes stops the run naming the worktree, which is kept. So does a work-branch checkout that picked up uncommitted changes during the run, naming that checkout. A task whose branch the work branch cannot fast-forward to stops the run naming the work branch. Either way the work branch is unchanged, the task stays closed, and nothing is pushed. The [worktrees ADR](./adr/0013-the-loop-works-in-task-worktrees.md) and the [worktree preparation ADR](./adr/0019-worktree-preparation.md) have the reasoning.

### Pushing

After each task lands, and before the loop chooses the next, it pushes the work branch to `origin` under the same name, setting that as the branch's upstream so a later bare `git push` works. It writes `smith: pushed #N on <branch> to origin` to stderr. The agent never pushes; the loop does. Task branches are never pushed. The loop never forces a push, and a task the agent closed without committing pushes nothing new, which is fine.

- **A failed push stops the run** (auth, a rejected push, the network), exiting non-zero and naming the task and git's error. The task stays closed, because its work is committed and landed, and no further task starts, so unpushed work cannot pile up. Fix the cause and push by hand, or run the loop again.

Pushing is on unless the repo file sets `push: false`; `--no-push` turns it off for one run, and the work branch stays local. `--interactive` lands and pushes the same way once its task is closed.

### `--interactive`

Hands the next available task to one agent run attached to your terminal, in the task's worktree like any other run, then stops. The prompt gains a note asking the agent to check its plan with you, ask rather than guess, and show you the result before it commits and closes the task. Approvals are the agent's own, so you answer them as you would in any session. The limits play no part. It reports `task #N closed` and exits 0, or `task #N still open` and exits 1. With no task available it runs no agent, prints the same stop report as `run`, and exits 0 only if the spec is complete.

Use it for the first run on a new repo or prompt, or for a task you want to steer.

### On a box: `--repo`

On a box, `smith loop run <issue> --repo <name>` runs the loop in a **loop-driven session** instead of in your terminal, so the run outlives your SSH connection ([ADR-0017](./adr/0017-the-loop-starts-its-own-session-on-a-box.md)). `<name>` is a repo the box's blueprint declares. It works in four steps:

1. **Preflight, in your terminal.** smith resolves the settings from the flags and the repo file, and checks the ejected prompt, as the session's root process will find them: in the work branch's worktree when a stopped session left one, uncommitted changes included; else at the work branch's tip when the clone holds it; else on the base the branch will be cut from, fetched first (`--base`, else the base the blueprint declares, else the clone's default branch, as origin holds it). It reads and admits the issue through `gh` in the repo's clone, and finds the agent on the `PATH` the session's root process will have: under the same `mise exec`, in the work branch's worktree, or in the repo's directory while that worktree does not exist yet. Any refusal is reported here, exactly as a foreground run reports it, and nothing is created: no session, no worktree, no work branch.
2. **The work branch** is `--branch`, else `smith/<n>-<slug>` from the issue (see [The work branch](#the-work-branch)). The session is named after it the way every session is: `<repo>-<branch>`, so spec #305 in repo `smith` runs in session `smith-smith-305-<slug>`, and `--branch feat/x` in `smith-feat-x`.
3. **One session per branch.** A live session on the work branch, human-driven or loop-driven, refuses the run, naming the session and the commands to attach to it or stop it. A stopped one hands over its worktree: the loop-driven session starts in it, and the loop adopts the branch.
4. **The session starts and smith returns.** The session's root process is `smith loop run <issue>` with the same flags, no `--repo`, and `--branch` set to the work branch, so the session ends when the loop does. smith prints the session's name and the commands to watch it, and exits 0:

```text
session smith-smith-305-run-the-loop-on-a-box is live on branch smith/305-run-the-loop-on-a-box of smith, driven by the loop
attach: smith session attach smith-smith-305-run-the-loop-on-a-box
list:   smith session list
```

`smith session attach` watches the run read-only, and the stop report lands on the issue as a comment (see [`smith loop run`](#smith-loop-run)). When the run stops, `smith session list` shows its outcome under `OUTCOME` (see [sessions](./sessions.md#loop-driven-sessions)), and `smith session stop` ends it as cleanly as Ctrl-C, with the outcome `stopped`. When only tasks for a human are left, start a human session on the same work branch with `smith session start --repo <name> --branch smith/<n>-<slug>`, which reuses the worktree, work those tasks, stop it, and run `loop run --repo` again.

`--repo` is refused off a box, where the loop runs in the repo you invoke it from. `--interactive` is refused with `--repo`: an interactive run needs you at its terminal, so start a human session and run `loop run --interactive` in it. Without `--repo`, a run on a box works in the foreground in the directory you invoke it from, as it does anywhere.

### From your machine: `loop run <box>`

`smith loop run <box> <issue> --repo <name>` from your own machine relays the run over SSH to the box's smith, the way the `session` verbs do. The box runs `smith loop run <issue> --repo <name>` with every loop flag you set (`--agent`, `--model`, `--effort`, `--max-iterations`, `--max-attempts`, `--branch`, `--base`, `--no-push`), so it starts the same loop-driven session as above. The preflight output and the exit status come back to your terminal, and smith returns once the session is live. It never attaches: run the printed `smith session attach <box> <name>` to watch.

A box running a different version of smith is handled as for the `session` verbs: asked about at a terminal, refused with the `smith machine upgrade <box>` to run otherwise. `--repo` is required with a box, and `--interactive` is refused with one before anything is relayed.

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
prepare: uv sync
labels:
  agent: ready-for-agent
  human: ready-for-human
  spec: spec
```

`smith repo init` writes a commented starter, filling in any value you give as a flag, and reports `created <path>`. It never overwrites an existing file: it reports `left alone <path>` instead. Outside a git repo it refuses: `must be run inside a git repo`.

Each setting resolves on its own, most specific first:

1. A flag on `smith loop run`, for that run only.
2. The repo file.
3. The built-in default: `claude`, the agent's own model and effort, push on, and the triage labels shown above.

The triage labels and the prepare command have no flags: they are set in the repo file or left to their defaults.

The file is validated strictly: an unknown key is refused with its line, and so is a label under `labels:` set to no name or to a name another key under it already has, and a `prepare:` that is not one non-empty string. An unknown agent or an effort off the scale is refused when the loop resolves it, naming where the value came from (`agent (repo file): …`); a flag that overrides the bad value hides it. `repo init` writes the values you give it without checking them. A repo whose `.smith/` is your config home (a home directory kept in git, say) is refused, so your own config and a repo's never share a directory. The [repo file ADR](./adr/0012-the-repo-file.md) has the reasoning.

### The prepare command

`prepare:` is the command that makes a checkout of the repo ready to work in, such as `uv sync`, `npm ci` or `make setup`. Left unset, no command runs. smith runs it when it [prepares a worktree](./sessions.md#worktree-preparation), with `sh -c` in the worktree, after installing the toolchain the repo's mise config pins and under `mise exec`, so it sees those pinned versions. A failed toolchain install stops before the command runs; a failed command is reported as the prepare step with its exit status. Where there is no mise, the toolchain step is skipped and the command runs in your own environment.

The command is read from the repo file in the worktree being prepared, not from the checkout you run smith in, so a branch that changes `prepare:` is prepared its own way.

The command must be safe to re-run. smith runs it every time it prepares a worktree, including one it prepared before, so it should bring an existing checkout up to date rather than assume a fresh one.

### Triage labels

`labels:` names the three labels the loop reads and writes on GitHub, for a repo whose triage uses other names:

| key | default | marks |
| --- | --- | --- |
| `agent` | `ready-for-agent` | a task for the agent |
| `human` | `ready-for-human` | a task for a human, and where a skipped task goes |
| `spec` | `spec` | a spec not yet split into tasks |

Each key is optional, and one left out keeps its default. A name set to nothing (`agent: ""`) is refused, never taken as the default. No two keys may share a name, the default ones included: a task carrying both the agent and the human label goes to the human, so `human: ready-for-agent` would send every task to a human. The loop uses the names everywhere it reads, writes or reports a label, such as `#N is not labelled "<agent label>"`, and `list` and `run` add a line to the settings block naming all three:

```text
labels: agent go (repo file), human ready-for-human (built-in default), spec epic (repo file)
```

`smith:working` and `smith:failed` keep their names, and no triage label may take either one: the loop marks its own work with them, so they never say who a task is for.

Before any agent runs, `run` reads the repo's labels once and writes a `smith: warning: …` line to stderr for each label the repo file sets that the repo does not have, then carries on:

- **`agent` or `spec`**: no issue will match it, and smith never creates it, because the loop only reads these.
- **`human`**: smith creates it the first time it skips a task, as it does the default.

A label left to its default is never checked, so a repo file with no `labels:` reads no labels at all. If the labels cannot be read, `run` warns that it could not check them and carries on.

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
