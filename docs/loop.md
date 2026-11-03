# The loop

The **loop** works a spec's tasks, or a single task, with a coding agent, one task per agent run. It asks the tracker which task is next, hands exactly that task to an agent, and reads the tracker again afterwards to see whether the agent closed it. The loop decides when it is done, not the agent.

```text
smith loop list <issue> [--branch <name>] [--base <branch>]
smith loop run  [<box>] <issue> [--preset <name> | --agent <name> [--model <name>] | --model <name>] [--effort low|medium|high]
                        [--max-iterations <n>] [--max-attempts <n>] [--interactive] [--no-push] [--no-review]
                        [--branch <name>] [--base <branch>] [--repo <name>]
smith loop prompt [work|review]
smith repo init [--agent <name>] [--model <name>] [--effort low|medium|high]
```

`loop list` and `loop run` work in the repo they are invoked from, which must be a checkout with a working tree, because they read the repo file and prompt from it. Run from a bare clone, either stops, saying the directory is a bare repo. Run the verb from a checkout of that repo instead, or, on a box, use `loop run --repo`, which starts the run in a worktree of its own (see [On a box](#on-a-box---repo)). `loop prompt` reads no repo and runs anywhere. No agent works in your checkout, though: `run` works each task in a worktree of its own, off the work branch (see [The work branch](#the-work-branch)), and leaves the branch you stand on and its uncommitted changes alone. It never commits itself. The prompt asks the agent to commit, so a failed attempt's changes are still in its task worktree for the next attempt. After each task the agent closes, the loop brings its commits onto the work branch and pushes that to `origin` (see [Pushing](#pushing)), so closed work never exists on one machine only.

## What the loop reads from GitHub

The tracker is GitHub Issues, reached through `gh`, so `gh` must be installed and authenticated. When `gh` is not logged in or GitHub refuses its token, `list` and `run` stop before touching any task and exit 1 with `the tracker can't authenticate with gh: <what gh said>`; any other `gh` failure reads `gh could not be used: <what gh said>`.

- **The issue** the loop is given is named as `42`, `#42`, `owner/name#42` or its issue URL, and is a spec or a single task. A qualified reference or URL for an issue in a different repo from the one `gh` sees in the working directory is refused.
- **A spec** is an issue with sub-issues, whatever its labels, or one labelled `spec`, which marks a spec not yet split into tasks. An issue with sub-issues is a spec even when it is labelled `ready-for-agent`, and is never handed to an agent itself. Any other issue is a task, worked alone as a run of one task. A **child task**, one with a parent, runs on its immediate parent's work branch, and no sibling task is worked. A **standalone task**, with no parent, runs on a work branch of its own.
- **Its tasks** are its sub-issues. The ones its `## Tasks` checklist lists come first, in that order, then any other sub-issue in the order GitHub holds them.
- **Who a task is for** comes from its labels: `ready-for-agent` tasks go to the agent, and `ready-for-human` tasks are never handed out by an unattended run: a human must be present, so only [`--interactive`](#--interactive) naming the task on its own pairs on one. A task with both labels is kept for the human. A task with neither is not ready yet, and a sub-issue that is itself a spec, labelled `spec` or with sub-issues of its own, is a nested spec, never a task. These are the default names of the **triage labels**: a repo can name its own (see [Triage labels](#triage-labels)), and every message names the labels the repo uses.
- **Blockers** are a task's GitHub "blocked by" links, plus any issue named in its `## Blocked by` section. A task is **available** when it is open, for an agent, and every blocker is closed.

A task named on its own must be available, or with `--interactive` open, unblocked and labelled for an agent or a human. Otherwise `run` launches no agent, sets up no work branch or worktree, and exits 1 naming why: `#N is closed`, `#N is meant for a human; pair on it with --interactive`, `#N is not labelled "ready-for-agent"` or `#N is blocked by #M`. There is no override. `list` still shows the task under where it stands, such as `blocked: #N (by #M)`, and exits 0.

A spec with no tasks yet is never handed to an agent: `run` sets up no work branch or worktree and exits 1 with `#N has no tasks`.

The loop hands out the first available task in the spec's order. It reads the tracker again before every choice, so a task filed or unblocked mid-run is seen.

## `smith loop list`

Shows the settings a run would use, the work branch and base branch it would use, the next task, and what remains, without running anything:

```text
preset: opus (repo file)
agent:  claude (repo file)
model:  opus (repo file)
effort: high (repo file)
push:   on (built-in default)
branch: smith/244-spec-the-loop-smith-loop-and-the-repo-file
base:   main
spec #244 Spec: the loop — smith loop and the repo file
next: #253 Loop: smith runs its own loop — .smith/ config, prompt and docs
  blocked: #254 (by #253; preset deep)
  waiting on a human: #255
  closed: #245, #246, #247
```

Each setting names where its value came from. The first line is the preset the work role runs on (see [Presets and roles](#presets-and-roles)): its name, or `one-off` for a preset built from `--agent`, then its agent, model and effort, each with its own origin, so a `--model` or `--effort` flag shows against the preset it changed. A task whose `preset:` label chooses a preset of its own shows it after its number, beside any open blockers, as `#254 (by #253; preset deep)`; a task without one shows none. A kind of task with nothing in it is left out.

The `branch:` line is the work branch a `loop run` of the issue would use, and `base:` the branch it would cut a new work branch from. Both are derived as `run` derives them (see [The work branch](#the-work-branch)): `smith/<n>-<slug>` from the issue, or the existing branch of that number, unless `--branch` names one, and origin's default branch unless `--base` names one. A task of a spec shows its spec's work branch. `list` only reads: it creates no branch, locally or on origin, so an agent can ask where a spec's work will land before committing to it.

## `smith loop run`

Prints the same settings block as `list`, then works the spec or task unattended until no task is available. As it goes, it writes `smith: handing #N <title> to <agent> on preset <name> in <worktree>` to stderr before each agent run, naming the preset that task runs on, or `on a one-off preset` for one built from `--agent`, and `smith: the agent on #N ended with a failure: …` when one fails. At the end it reports why it stopped, then names the work branch (`work branch: smith/N-<slug>`):

- **`spec #N complete`**, or **`task #N complete`** for a task named on its own, child or standalone: every task is closed. Exit 0.
- **No task is available for an agent**: what remains is blocked, for a human, or not ready. Exit 1.
- **The agent left a task open on every attempt**: see attempts below. Exit 1.
- **The agent failed on every attempt at a task**, or **could not be started**: see agent failures below. Exit 1.
- **Reached the cap of N agent runs**: see iterations below. Exit 1.
- **The review failed**: see [the review](#the-review). Exit 1.
- **The review is incomplete**: see [the review](#the-review). Exit 1.

However the run stops, the loop also posts its stop report as a comment from smith on the issue it was given: the spec, or the task for a run of one task. The comment names the run's **outcome**, then carries the report and its `work branch:` line, so you can read how a run ended from anywhere, and GitHub notifies whoever follows the issue ([ADR-0017](./adr/0017-the-loop-starts-its-own-session-on-a-box.md)). The outcome is one of:

- **`complete`**: every task is closed and, when the repo turns the [review](#the-review) on, the review is done.
- **`review failed`**: the review ran out of attempts without a valid findings file, or committed or changed files in its worktree. The report says why and where the review worktree is. Neither review label is left, so a later run reviews again.
- **`review incomplete`**: a tracker failure while filing the review's tasks, editing its ledger or marking the issue. The loop stops at once without retrying and removes `smith:reviewing`; the report says the ledger is posted and N of M findings are filed. A later run finishes the filing (see [the review](#the-review)).
- **`waiting on human`**: no task is available for an agent.
- **`attempts exhausted`**: the agent left a task open on every attempt.
- **`cap reached`**: the run reached its cap of agent runs.
- **`agent broken`**: the agent failed on every attempt at a task, or could not be started. The report names the task, the agent's last error and the machine.
- **`stopped`**: you interrupted the run, such as with Ctrl-C. The report says `interrupted`.
- **`failed`**: the run stopped on an error, such as a failed push. The report names the error.

<a id="task-worktree-placements"></a>The loop first places the repo's files into the task worktree: the [placements](./sessions.md#start-is-ensure-running-not-create) a blueprint declares for the repo the run works in. On a box the bytes are what `machine setup` staged. Off a box, where nothing is staged, smith reads every blueprint in your config home, takes the repo entries whose `url` names the same repo as the checkout's `origin` (SSH and HTTPS forms match, ignoring case and `.git`; the repo name alone never matches), and combines them: the placements of every matching entry, across all the blueprints, are joined by destination path and each path is placed once, and where entries disagree on a path's mode `converge` wins. A blueprint that cannot be read or parsed is skipped with one `smith: warning:` line naming the file, and the run goes on with the rest; no source reference is resolved. smith copies the same paths from the top level of the worktree the loop was started in, so a linked worktree is its own source and a subdirectory resolves to its top level. The copy is a whole file, never a link, and the placement's `perms` apply. A repo no blueprint declares places nothing and prints nothing. It does so before every attempt and before preparation, so `prepare:` and the agent both see them, in a new worktree, a recycled one and one an earlier attempt left. A write-once placement is written when absent and never overwritten. Every placement destination must be gitignored in the repo: before any task is marked or any agent runs, the loop checks them with `git check-ignore`, and stops with an error naming each path that is not ignored, asking you to add it to the repo's `.gitignore`. Otherwise a placed secret could be committed by an agent, or a tracked file overwritten into the diff. Off a box the loop also checks each source before any task is marked, and stops with an error when a source is missing from the checkout (naming the path, the checkout and the blueprint that declares it) or is a file the repo tracks (a tracked file the loop will not copy: it is already in the worktree, and copying it would carry uncommitted edits into the task), or is not gitignored in the checkout (asking you to add it to the checkout's `.gitignore`). On a box there is no source, so no source check runs. On Windows only the read-only bit of `perms` takes effect, and a read-only placement cannot be replaced by a later `converge`.

Before every agent attempt, the first and each retry, the loop [prepares](./sessions.md#worktree-preparation) the task worktree, on your machine as on a box: it installs the toolchain the repo's mise config pins, then runs the worktree's [prepare command](#the-prepare-command). It writes one line, `smith: preparing the worktree of #N`, to stderr and holds both steps' output back; only if a step fails does it print the last 64 KiB of that output, ahead of the failure report. The agent then runs under `mise exec` in the task worktree, so it sees the toolchain and env that worktree's mise config pins, even where they differ from the session worktree's on a box. mise trusts the repo's config in the task worktree smith cut, for preparation and the agent alike, for the reason [worktree preparation](./sessions.md#worktree-preparation) gives, so a `mise` the agent runs itself accepts it too. On a box, the loop-driven session's process already trusts the repo's whole directory, its task worktrees included. Where there is no mise, the agent runs directly in your own environment. A failed preparation launches no agent and stops the run with the outcome `failed`, its report naming preparation, the task and the task worktree (`prepare the worktree of #N in <worktree>: …`). Another attempt cannot fix it, so it counts as no attempt: the task is not labelled `smith:failed`, skipped or commented on. A loop-driven session's own worktree is not prepared, since only smith runs there.

A stop report that fails to post is a `smith: warning: …` line, and changes neither the outcome nor the exit status. A run refused before its issue is admitted (an issue that cannot be read, a task that is not available, a spec with no tasks, an agent that is not installed, an invalid repo file or prompt) posts nothing; one that fails after that, such as on setting up the work branch, posts the outcome `failed`. `--interactive` posts no stop report.

Two limits bound a run so it can be left alone:

- **`--max-attempts`** (default 2): a task the agent leaves open is handed out again until it has had this many runs, then skipped for the rest of the run. A skipped task stays open on GitHub, usually with the agent's WIP note on it, and is handed to you: the loop moves it from `ready-for-agent` to `ready-for-human` and labels it `smith:failed`. Moving it back from `ready-for-human` to `ready-for-agent` sends it back to the loop, which removes `smith:failed` when it next hands the task out. Its task worktree and task branch are left for you: the report names the worktree (`#N left in <worktree>`), and the loop comments on the task's issue with the machine's hostname, the worktree's path, the task branch, the preset its attempts ran on, the attempts it made in this run, and how many commits the task branch has beyond the work branch ([ADR-0016](./adr/0016-the-loop-comments-on-a-task-it-skips.md)). A hostname or commit count it cannot read shows as unknown, and a comment that fails to post is a `smith: warning: …` line, as a failed label write is.
- **`--max-iterations`** (default 10): the most agent runs the whole loop makes.

An agent that exits with a failure is reported, and the task is checked on the tracker as after any run: a task it closed lands, whatever the exit code. Whether the loop carries on depends on its attempts, as below.

A run the agent CLI ends with a non-zero exit is an **agent failure**: it says the agent may be broken, not the task. When every attempt at a task is an agent failure, the run stops as `agent broken` instead of skipping it, so a dead credential or a broken agent install stops the run on its first task rather than handing every task of the spec to you. A launch that cannot start the agent at all, such as a binary missing from `PATH`, stops the run the same way on its first launch, counting no attempt. The task the run stops on is left as the next run should find it: its `smith:working` label is removed, it is not labelled `smith:failed`, handed to you or commented on, and its task worktree is kept. The stop report reads like `spec #42 stopped: the agent failed on every attempt at #44 (run claude: exit status 1); check that the agent runs on <hostname>`, the hostname marked unknown when it cannot be read. A task with even one attempt that was not an agent failure, a clean exit that left it open or a SIGKILL, is skipped as usual. Tasks closed earlier in the run stay landed and pushed. `--interactive` is unchanged: it makes one attempt and never skips ([ADR-0020](./adr/0020-the-loop-stops-on-a-broken-agent.md)).

Ctrl-C stops the running agent and starts no further task.

While an agent runs on a task, the loop labels the task `smith:working` on GitHub, and removes the label after every attempt, whether the agent closed the task, left it open or used its last attempt, when you interrupt the run, and when the tracker cannot be read after the agent exits, so you can see on the tracker which task is being worked. A run that is killed, or whose machine is lost, can leave the label behind, so an unattended run first removes `smith:working` from every task it is about to work, the spec's or the one task named, and from no other issue. The labels are signals for people: the loop never reads them to choose a task ([ADR-0015](./adr/0015-the-loop-marks-tasks-as-a-signal-not-a-claim.md)). A repo without one of these labels gets it created the first time it is needed. A label write that fails costs no work, so it is a `smith: warning: …` line on stderr naming the task, and the run carries on. `--interactive` marks its task the same way, but never hands a task to a human: a task it leaves open keeps the triage label it had, `ready-for-agent` or `ready-for-human`.

### The review

A repo turns the review on by setting the `review` role in its [repo file](#presets-and-roles). With it unset, nothing is ever reviewed.

An unattended run reviews when `roles.review` is set, every task is closed, and the issue is not labelled `smith:reviewed`. So it reviews once, after the run has worked the last task, and a run on a spec whose tasks were all closed earlier only reviews. It does not review a spec with a task left for a human, blocked or skipped, a child task run on its own (its spec's run reviews the whole branch), or an `--interactive` run. A task with no parent reviews as a spec of its own. A child task run on its own that leaves its spec with every task closed and unreviewed ends its report with a `hint:` line saying `smith loop run <spec>` reviews it; there is none while the spec has open tasks, once it is reviewed, or with `roles.review` unset. A standalone task is reviewed on its own work branch and its review files tasks as its sub-issues; the run then reads it as a spec of the same number on the same branch and works those tasks, and the task's own issue stays closed throughout.

The review runs one agent, on the preset `roles.review` names; a task's `preset:` label never chooses it. Any of `--preset`, `--agent` or `--model` pins the review with the rest of the run, to the preset the work role would run on under those flags; `--effort` alone changes only the review's effort and keeps its preset. The flags never turn the review on: that is `roles.review`.

`--no-review` skips the review for one run. The run reviews nothing and leaves no mark, so a later run reviews; it ends `complete`, and its report says `not reviewed: `--no-review``. On a complete, unreviewed execution it has nothing to do and ends `complete`. It is refused with `--interactive` before anything is read, and a start-up check of the review's agent is skipped with it. The agent works in the recycled worktree, prepared like a task's, checked out detached at the work branch's tip with no task branch, so it has nothing to land. It is handed the [review prompt](#the-review-prompt) with the spec's number and title, the merge-base of the work branch and the base branch as a commit, and the absolute path of the findings file. It never commits or changes the tracker.

The agent writes its findings to that file, a JSON object with a `findings` list and a `not_filed` list, under the git directory's `smith/` directory and keyed by issue number. The loop deletes any stale file before each attempt, and an attempt succeeds when the agent exits cleanly and leaves a valid file. A review gets `--max-attempts` attempts, each counting against `--max-iterations`; a review that would start past the cap waits for the next run, which only reviews, and so do the tasks the review files, which count against the cap too. An attempt that exits 0 without a valid file fails with the validation error in its report, and nothing is filed or posted. When every attempt is out the run ends `review failed` with neither label left; every attempt being an agent failure, or an agent that cannot be started (which counts no attempt), ends `agent broken` instead, as for a task ([ADR-0020](./adr/0020-the-loop-stops-on-a-broken-agent.md)).

After each attempt the loop checks that the review's worktree is clean and HEAD is still the work branch's tip. A review that made a commit or left changed files ends the run `review failed` at once, with no retry: the worktree is kept, no label is left, and the report names the stray commits or changed files.

The loop then files the findings. It first posts the review as a comment on the issue, the ledger: a section each for Spec, Standards and Quality (each "No findings." when empty), the restructurings under **Not filed**, a tally with the worst finding, and a hidden `<!-- smith:review … -->` block holding the findings and the work branch sha reviewed. Every finding is given an id from file order per axis (S1, St1, Q1) and becomes exactly one sub-issue of the issue, filed Spec, then Standards, then Quality, with a finding always after those it is `blocked_by`. A task carries the repo's agent or human triage label as its finding's `who` says, and its body is the finding's body ending in a Source line: `Source: Quality axis, major, finding Q1, review of #42`. A blocker becomes a blocked-by link; on a `gh` without `--parent` or `--blocked-by`, the task body starts with `Part of #42` and `Blocked by #<task>` lines instead. A restructuring under **Not filed** is never filed. Once every task is filed the loop edits the comment once to name each finding's task, then labels the issue `smith:reviewed`. A `gh` failure while filing, editing the comment or marking stops the run at once, without retrying, and ends it `review incomplete`, with the issue left without `smith:reviewed`.

A run that finds a ledger on an issue not labelled `smith:reviewed` runs no review agent: it resumes. It reads which findings a sub-issue of the issue already names (by the finding id in its Source line), files only the rest, edits the comment to name every task and labels the issue `smith:reviewed`, and then works the agent tasks as usual. This holds on any machine, and when the work branch has moved since the ledger's sha: the findings are filed anyway and the report notes the move. A ledger whose hidden block cannot be parsed stops the run before any agent runs, naming the comment. To review again after a review is done, remove `smith:reviewed` and delete the ledger comment.

The run then re-reads the issue and works the agent tasks the review filed, as it works any task, and ends `complete`, or `waiting on human` when it filed only human tasks. It does not review a second time. The printed report and the stop report name the filed tasks with their finding ids. Remove `smith:reviewed` and delete the ledger comment by hand to review again.

While the review runs the issue carries `smith:reviewing`, removed on every way the review ends, and cleared by the start-up sweep when a killed run left it. Both labels are created the first time they are needed. A failed write of either label is a `smith: warning: …` line, and the review carries on.

### The work branch

Each run's work lands on its **work branch**, `smith/<n>-<slug>`: the number of the spec, of a child task's immediate parent, or of the standalone task, then its title lower-cased, with each run of other characters made one hyphen, without a leading `Spec:`, and cut at a word to at most 40 characters. Spec #430 titled "Spec: Loop works in task worktrees" works on `smith/430-loop-works-in-task-worktrees`. `--branch` names the work branch instead, used exactly as written, and no branch is then looked up by number. An existing work branch is adopted, found by number (`smith/<n>-*`) so an issue renamed since keeps its branch: a local one is reused as it is, and one only on `origin` is created locally from `origin`'s copy. An adopted branch is then compared with the tip of its base on `origin`, the branch `--base` names or `origin`'s default branch: when the base has commits the branch lacks, the run prints a `smith: warning: …` line on stderr before any agent runs, naming the branch, the base and how many commits behind it is, with the fix `merge or rebase origin/<base> into it`, and carries on. The loop never brings those commits in itself. A comparison that cannot run, such as a base `origin` does not have, is a `smith: warning: …` line saying why, and the run carries on too. A freshly cut branch is level with its base, and with no base at all there is nothing to compare, so neither is checked. That is how a branch holding the spec's docs reaches the loop. It is also how runs share a branch: `loop run 82`, for a child of spec #74, adopts `smith/74-<slug>` or cuts it, and a later `loop run 74` adopts it again and works only the tasks still open. The run stops before any agent runs when more than one branch matches the number, naming each, or when the local and `origin` copies have diverged, naming the branch. When no branch matches, the loop cuts one from the tip of the branch `--base` names on `origin`. Without `--base`, it cuts it from the tip of `origin`'s default branch, which it asks `origin` for with `git ls-remote --symref origin HEAD`, so the lookup also works for a run with `--repo` on a box, whose `repo.git` is a bare clone with no `origin/HEAD`. If `origin` names no default branch and no `--base` is given, the run stops before any agent runs, saying how to give `origin` one. A work branch checked out in a worktree with uncommitted changes, such as your own checkout, also stops the run before any agent runs, naming that worktree. The work branch may be neither `origin`'s default branch nor the base: a run whose work branch is either stops before any agent runs, and nothing is pushed.

Each task is worked in a **task worktree** on a **task branch**, `smith/tasks/<work-branch>/<task>`, cut from the work branch's tip. Where task worktrees live depends on whether the repo is bare, with directories nested along the work branch's `/` under either root so no two work branches share one:

- **A bare repo**, such as a box's `repo.git`, a run in a box session's worktree included: `<git-common-dir>/smith/worktrees/<work-branch>/`.
- **A non-bare repo**, such as an ordinary clone on your machine: `<state home>/worktrees/<repo>-<hash>/<work-branch>/`. The state home is `$XDG_STATE_HOME/smith/` when that is an absolute path, else `~/.local/state/smith/`, on every OS. `<repo>` is the checkout's directory name and `<hash>` a short hash of its git directory, so two clones of one repo stay apart. Inside the checkout's `.git`, a task worktree looks empty to tools that skip `.git` paths, such as Jest and Metro, and config lookup (ESLint, Prettier, Babel) climbs into the checkout's own.

A non-bare repo's task worktrees outlive a checkout that is deleted or moved, and smith doesn't clean them up.

A task worktree is **recycled**: once a task lands, the next task's branch is switched into the same worktree, so what the repo ignores (a `.venv`, `node_modules`) carries over and preparation runs incrementally. A new worktree is cut only when there is none to recycle, at `<work-branch>/<task>` for the task it is cut for. Anything already at that path, such as a directory git does not know or a worktree on another branch, stops the run and is left in place. A task the agent leaves open keeps its worktree and branch, so the next attempt continues where the last stopped. A skipped task's are left for a human, so the task after it gets a new worktree. A run that ends `complete` removes the recycled worktree, with `git worktree remove`, so one holding changes is kept with a `smith: warning: …` line. A run that ends any other way keeps it, and the next run of the same execution works its first task in it. Once the tracker says a task is closed, the loop fast-forwards the work branch to the task branch, with `git merge --ff-only` in the worktree that has the work branch checked out if one does, so a checkout of the work branch follows the loop, then detaches the task worktree at the landed commit and removes the task branch. A task closed without commits lands as a no-op, its branch still removed. A closed task whose worktree holds uncommitted changes stops the run naming the worktree, which is kept. So does a work-branch checkout that picked up uncommitted changes during the run, naming that checkout. A task whose branch the work branch cannot fast-forward to stops the run naming the work branch. Either way the work branch is unchanged, the task stays closed, and nothing is pushed. The [worktrees ADR](./adr/0013-the-loop-works-in-task-worktrees.md), the [state home ADR](./adr/0021-task-worktrees-of-a-non-bare-repo-live-in-the-state-home.md) and the [worktree preparation ADR](./adr/0019-worktree-preparation.md) have the reasoning.

### Pushing

After each task lands, and before the loop chooses the next, it pushes the work branch to `origin` under the same name, setting that as the branch's upstream so a later bare `git push` works. It writes `smith: pushed #N on <branch> to origin` to stderr. The agent never pushes; the loop does. Task branches are never pushed. The loop never forces a push, and a task the agent closed without committing pushes nothing new, which is fine.

- **A failed push stops the run** (auth, a rejected push, the network), exiting non-zero and naming the task and git's error. The task stays closed, because its work is committed and landed, and no further task starts, so unpushed work cannot pile up. Fix the cause and push by hand, or run the loop again.

Pushing is on unless the repo file sets `push: false`; `--no-push` turns it off for one run, and the work branch stays local. `--interactive` lands and pushes the same way once its task is closed.

### `--interactive`

Hands the next available task to one agent run attached to your terminal, in the task's worktree like any other run, then stops. The prompt gains a note asking the agent to check its plan with you, ask rather than guess, and show you the result before it commits and closes the task. Approvals are the agent's own, so you answer them as you would in any session. The limits play no part. It reports `task #N closed` and exits 0, or `task #N still open` and exits 1. With no task available it runs no agent, prints the same stop report as `run`, and exits 0 only if the spec is complete.

Naming a task meant for a human on its own, `loop run --interactive <task>` pairs you with an agent on it, in the same way: naming it at your terminal is the consent, so there is no extra flag. It runs on the work branch the task would use anyway, its parent spec's or its own, in its task worktree, so a task the loop skipped and handed to you continues in the worktree it left, and loses its `smith:failed` label when handed out. Closed by the agent or by you, it lands and is pushed. Left open, it stays yours: still `ready-for-human`, never labelled `smith:failed` and never commented on. A closed, blocked or unlabelled task is still refused, and on a spec `--interactive` still hands out only its next task meant for an agent.

Use it for the first run on a new repo or prompt, for a task you want to steer, or to pair on a task meant for a human.

With no terminal attached, `--interactive` refuses at once and exits 1, before it reads the tracker or launches an agent, and names the same run without `--interactive`.

### On a box: `--repo`

On a box, `smith loop run <issue> --repo <name>` runs the loop in a **loop-driven session** instead of in your terminal, so the run outlives your SSH connection ([ADR-0017](./adr/0017-the-loop-starts-its-own-session-on-a-box.md)). `<name>` is a repo the box's blueprint declares. It works in four steps:

1. **Preflight, in your terminal.** smith resolves the settings from the flags and the repo file, and checks the ejected prompt, as the session's root process will find them: in the work branch's worktree when a stopped session left one, uncommitted changes included; else at the work branch's tip when the clone holds it; else on the base the branch will be cut from, fetched first (`--base`, else the base the blueprint declares, else the clone's default branch, as origin holds it). It reads and admits the issue through `gh` in the repo's clone, and finds the agent on the `PATH` the session's root process will have: under the same `mise exec`, in the work branch's worktree, or in the repo's directory while that worktree does not exist yet. Any refusal is reported here, exactly as a foreground run reports it, and nothing is created: no session, no worktree, no work branch.
2. **The work branch** is `--branch`, else `smith/<n>-<slug>` from the issue (see [The work branch](#the-work-branch)). The session is named after it the way every session is: `<repo>-<branch>`, so spec #305 in repo `smith` runs in session `smith-smith-305-<slug>`, and `--branch feat/x` in `smith-feat-x`.
3. **One session per branch.** A live session on the work branch, human-driven or loop-driven, refuses the run, naming the session and the commands to attach to it or stop it. A stopped one hands over its worktree: the loop-driven session starts in it, and the loop adopts the branch.
4. **The session starts and smith returns.** The session's root process is `smith loop run <issue>` with the same flags, no `--repo`, and `--branch` set to the work branch, so the session ends when the loop does. smith prints the settings block, as `list` does, then the session's name and the commands to watch it, and exits 0:

```text
preset: opus (repo file)
agent:  claude (repo file)
model:  opus (repo file)
effort: high (repo file)
push:   on (built-in default)
session smith-smith-305-run-the-loop-on-a-box is live on branch smith/305-run-the-loop-on-a-box of smith, driven by the loop
attach: smith session attach smith-smith-305-run-the-loop-on-a-box
list:   smith session list
```

`smith session attach` watches the run read-only, saying so in your terminal, and `--interact` jumps in writable when it needs a hand. The stop report lands on the issue as a comment (see [`smith loop run`](#smith-loop-run)). When the run stops, `smith session list` shows its outcome under `OUTCOME` (see [sessions](./sessions.md#loop-driven-sessions)), and `smith session stop` ends it as cleanly as Ctrl-C, with the outcome `stopped`. When only tasks for a human are left, start a human session on the same work branch with `smith session start --repo <name> --branch smith/<n>-<slug>`, which reuses the worktree, work those tasks, by hand or paired with an agent through `loop run --interactive <task>` in the session, stop it, and run `loop run --repo` again.

`--repo` is refused off a box, where the loop runs in the repo you invoke it from. `--interactive` is refused with `--repo`: an interactive run needs you at its terminal, so start a human session and run `loop run --interactive` in it. Without `--repo`, a run on a box works in the foreground in the directory you invoke it from, as it does anywhere.

### From your machine: `loop run <box>`

`smith loop run <box> <issue> --repo <name>` from your own machine relays the run over SSH to the box's smith, the way the `session` verbs do. The box runs `smith loop run <issue> --repo <name>` with every loop flag you set (`--preset`, `--agent`, `--model`, `--effort`, `--max-iterations`, `--max-attempts`, `--branch`, `--base`, `--no-push`, `--no-review`), so it starts the same loop-driven session as above. The preflight output and the exit status come back to your terminal, and smith returns once the session is live. It never attaches: run the printed `smith session attach <box> <name>` to watch.

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
presets:
  opus:  { agent: claude, model: opus,        effort: medium }
  deep:  { agent: claude, model: opus,        effort: high }
  codex: { agent: codex,  model: gpt-6.1-sol, effort: high }
roles:
  work: opus
push: true
prepare: uv sync
labels:
  agent: ready-for-agent
  human: ready-for-human
  spec: spec
```

`smith repo init` writes a commented starter and the repo guide, `.smith/README.md`, a short note that tells a coding agent working the repo that smith drives it. It reports each file as `created <path>`. The starter defines one preset, named for its agent, and points the work role at it: `--agent`, `--model` and `--effort` fill that preset in, and without them it is `claude` with the agent's own model and effort. `smith repo init --agent codex --model gpt-6.1-sol --effort high` writes `presets: {codex: {agent: codex, model: gpt-6.1-sol, effort: high}}` and `roles: {work: codex}`. A starter that would not load, such as one for an unknown agent, is refused and nothing is written. It never overwrites an existing file: it reports `left alone <path>` instead, and writes the other, so running it again in a repo that has a repo file adds the repo guide. It then offers to add one line, pointing at the repo guide, to the repo's agent instruction files, with one [y/N] question naming the files it will touch: `AGENTS.md` alone, a `CLAUDE.md` alone (at the root or in `.claude/`), both when the `CLAUDE.md` has no line that is exactly `@AGENTS.md` or `@./AGENTS.md`, and a new `AGENTS.md` when there is none. A file that already mentions `.smith/README.md` is skipped, and when every file is, nothing is asked. Answering no, or running with nobody at the terminal, changes no file and prints the line and `smith repo init --yes`, which adds it; `--yes` answers yes. Outside a git repo it refuses: `must be run inside a git repo`. In a bare clone it refuses too, saying the directory is a bare repo.

Each setting resolves most specific first:

1. A flag on `smith loop run`, for that run only.
2. The repo file.
3. The built-in default: the `default` preset, which is `claude` with the agent's own model and effort, push on, and the triage labels shown above.

The triage labels and the prepare command have no flags: they are set in the repo file or left to their defaults.

The file is validated strictly: an unknown key is refused with its line, and so is a label under `labels:` set to no name or to a name another key under it already has, and a `prepare:` that is not one non-empty string. The presets and roles are checked as [Presets and roles](#presets-and-roles) says. A repo whose `.smith/` is your config home (a home directory kept in git, say) is refused, so your own config and a repo's never share a directory. The [repo file ADR](./adr/0012-the-repo-file.md) has the reasoning.

### Presets and roles

A **preset** is a named agent, model and effort, chosen whole, so a model never outlives the agent it belongs to. A repo defines as many presets as it likes under `presets:`, each with an `agent` and, optionally, a `model` and `effort`. One that leaves out its model or effort leaves it to the agent's own default.

A **role** is a job smith hands an agent. smith defines the roles, and `roles:` says which preset each one runs on. `work` works every task, unattended or with `--interactive`. `review` reviews the whole work branch once, after the last task closes, and is off unless the file sets it (see [the review](#the-review)). With no `work` role, or no repo file, tasks run on the built-in `default` preset: `claude` with no model and no effort, even when the file defines presets.

Every preset is checked when the repo file loads, used or not, and each problem is refused with its line before any agent runs:

- its name is lowercase letters, digits and hyphens
- its agent is one smith has an adapter for (`claude`, `codex` or `pi`)
- its effort is on smith's scale (`low`, `medium`, `high`)

A role smith does not define (`smith has no role "audit": the roles are work, review`) and a role naming a preset `presets:` does not define are refused too. The flat top-level `agent`, `model` and `effort` keys of earlier versions are refused with their line and the presets shape that replaces them.

The run flags change the preset for one run, without editing the file:

- `--preset P` runs every task on the repo file's preset `P`, which the settings report shows as from the flag.
- `--agent X` builds a one-off preset on `X` that never inherits the repo file's model or effort, and `--model Y` beside it sets that preset's model.
- `--model Y` alone keeps the work preset's agent and effort, and replaces its model.
- `--effort E` replaces the effort of whichever preset the run resolves.

A task can run on a preset of its own: a `preset:<name>` label on its issue, such as `preset:codex`, sends it to the repo file's preset `<name>` while the rest of its spec stays on the work preset. The `preset:` prefix is smith's own, like `smith:working`, not a triage label a repo file can rename. A preset therefore comes from, most specific first:

1. `--preset`, `--agent` or `--model`, which pin every task of the run to one preset, over any label.
2. The task's `preset:` label.
3. The work role's preset.
4. The built-in `default` preset.

`--effort` alone pins nothing: each task keeps its own preset and runs at that effort. Straight after reading the spec, before any worktree is opened or agent started, the run checks each open task's label, and stops as `failed` on a task naming a preset the repo file does not define (`#N's label preset:missing names preset "missing", which the repo file does not define`) or carrying two labels (`#N carries the labels preset:opus and preset:codex: a task runs on one preset`). Closed tasks are not checked, and a run pinned by a flag checks no labels at all. `--interactive` honours the label the same way. Whether an agent is installed is checked up front only for the work preset's agent, so a labelled task whose agent is missing stops the run as `agent broken` when it is handed out.

An unknown `--agent`, an `--effort` off the scale, and a `--preset` the repo file does not define are refused, naming the flag. `--preset` beside `--agent` or `--model` is refused too, since a preset already names its agent and model. The [presets ADR](./adr/0022-presets-and-roles.md) has the reasoning.

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

Each key is optional, and one left out keeps its default. A name set to nothing (`agent: ""`) is refused, never taken as the default. No two keys may share a name, the default ones included: a task carrying both the agent and the human label goes to the human, so `human: ready-for-agent` would send every task to a human. The loop uses the names everywhere it reads, writes or reports a label, such as `#N is not labelled "<agent label>"`, and when the repo file sets any of them, `list` and `run` add a line to the settings block naming all three:

```text
labels: agent go (repo file), human ready-for-human (built-in default), spec epic (repo file)
```

`smith:working`, `smith:failed`, `smith:reviewing` and `smith:reviewed` keep their names, and no triage label may take any of them: the loop marks its own work with them, so they never say who a task is for.

Before any agent runs, `run` reads the repo's labels once and writes a `smith: warning: …` line to stderr for each label the repo file sets that the repo does not have, then carries on:

- **`agent` or `spec`**: no issue will match it, and smith never creates it, because the loop only reads these.
- **`human`**: smith creates it the first time it skips a task, as it does the default.

A label left to its default is never checked, so a repo file with no `labels:` reads no labels at all. If the labels cannot be read, `run` warns that it could not check them and carries on.

### The review prompt

The review has its own built-in prompt, `internal/loop/review.md`, with the placeholders `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{MERGE_BASE}}` and `{{FINDINGS_FILE}}`. It starts three subagents with fresh contexts, one for each of the Spec, Standards and Quality axes, and has the agent write what they return to the findings file.

Eject it like the loop prompt, with the role named:

```sh
smith loop prompt review > .smith/review.md
```

`smith loop prompt` takes an optional role, `work` by default; any other role than `work` and `review` is refused. Once `.smith/review.md` exists the review agent is handed it instead of the built-in, with the placeholders filled in. An unknown placeholder stops the run before any agent runs, and no placeholder is required. An ejected prompt must still make the agent write the findings file (see the agent guide for its contract).

pi has no subagents, which the built-in prompt relies on. An unattended run that would review, whose review preset runs pi (from `roles.review` or a pinning flag such as `--agent pi`), and that has no `.smith/review.md`, stops before any agent runs. Eject the review prompt and point it at a subagent tool, or pass `--no-review`.

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
