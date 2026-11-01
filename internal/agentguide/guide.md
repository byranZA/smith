# Agent guide

This guide is for an agent that drives smith: one that plans work and runs smith commands for the operator. An agent handed a task by the loop does not run smith; the loop prompt alone instructs it.

Each verb's `--help` has its examples and the exit codes it returns. `smith help exit-codes` lists every code.

## Mental model

The operator brings a **box**, a fresh Ubuntu VPS. `smith machine setup` provisions it: a `smith` user, a firewall, hardened SSH, and smith itself installed on the box. From then on the operator names the box and runs smith verbs against it from their own machine. Verbs that work on a box relay to the box's own smith over SSH.

Work on a box happens in **sessions**: a `tmux` session in a worktree on one branch. A session has one **driver**, a human (`smith session start`) or the loop (`smith loop run <box> <issue>`). Attaching shows either live.

The **loop** works one spec to completion. You write a **spec** on GitHub and split it into **tasks**, one sub-issue each. The loop hands one task at a time to a coding agent, checks afterwards whether the task closed, and merges each task branch into the spec's **work branch**. It pushes that branch and nothing else, and it reports how it ended as an **outcome**. The operator then reviews the work branch and proposes it back to the base branch.

## Glossary

- **Box**: the VPS smith provisions and the operator works on.
- **Session**: a `tmux` session in a worktree on one branch, on a box. The unit of work there.
- **Driver**: who acts in a session, a human or the loop. The verb that creates the session sets it.
- **Loop**: the driver that works one spec to completion, one task per agent run.
- **Spec**: the GitHub issue whose sub-issues are the tasks. The loop never hands a spec itself to an agent.
- **Task**: one unit of work, handed to an agent or a human. An agent gets exactly one task per run.
- **Work branch**: the spec's branch, the one branch the loop pushes. A human may commit to it before a run.
- **Task branch**: the branch one task is worked on, merged into the work branch once the task closes.
- **Outcome**: how a loop run ended, such as `complete`, `waiting on human`, `attempts exhausted` or `agent broken`.
- **Triage label**: the label saying what an issue is for: `ready-for-agent`, `ready-for-human` or `spec` by default.
- **Repo file**: `.smith/repo.yaml`, the repo's committed smith configuration.
- **Preset**: a named agent, model and effort, chosen whole and by name in the repo file.

## The issue shape the loop expects

- A **spec** is a GitHub issue with a sub-issue per task, in the order the loop should work them. A spec not yet split into tasks carries the `spec` triage label.
- Every task carries a triage label: `ready-for-agent` for one an unattended run may work, `ready-for-human` for one that needs a person present. A task with neither is not handed out.
- A task's **blockers** are its GitHub "blocked by" links, plus any issue named in a `## Blocked by` section of its body. A task is available when it is open, labelled `ready-for-agent` and every blocker is closed.
- The loop labels a task `smith:working` while an agent runs on it and removes the label afterwards. A task that used up its attempts is moved to `ready-for-human` and labelled `smith:failed`, with a comment saying where its worktree is. Moving it back to `ready-for-agent` sends it back to the loop. These two labels are signals for people; never set them by hand to steer the loop.
- A task body that stands alone is the whole brief. A task with a parent is read together with its spec.

## Recipes

### Set up a box

Describe the box, check it offline, then provision it. The operator supplies the address of a fresh Ubuntu box they can reach as root or a passwordless-sudo user.

```sh
smith init
smith blueprint check personal
smith machine setup root@203.0.113.10 --name dev --blueprint personal
smith machine status dev
```

`machine setup` runs long and is safe to re-run. `machine status` reports drift and changes nothing.

### Start a loop on a spec and walk away

In a checkout of the repo, write the repo file once and commit it, then check what the loop would do before starting it.

```sh
smith repo init --agent claude --model opus
smith loop list 42
smith loop run dev 42 --repo app
```

`loop run <box>` starts a loop-driven session on the box and returns; the loop runs on without you. A run in a checkout without a box name runs in the foreground, on this machine.

### Check on a run

```sh
smith session list dev
smith session attach dev app-smith-42-login-flow
smith loop list 42
```

`session list` shows each session's state and outcome. `session attach` watches a loop-driven session read-only. `loop list` shows each task's state on GitHub, where `smith:working` marks the one in progress.

### Pair on a ready-for-human task

Name the task on its own. The loop never works a `ready-for-human` task unattended; naming it at an interactive terminal is the consent.

```sh
smith loop run --interactive 44
```

The agent is attached to your terminal, on the same work branch and task worktree the loop would use. Closed, the task lands and is pushed. Left open, it stays yours.

### Commit a spec's design docs to its work branch before a run

Ask smith which work branch a run would use. `loop list` only reads, so it creates no branch.

```sh
smith loop list 42
```

The `branch:` line names the work branch (`smith/<n>-<slug>`) and `base:` the branch a new one is cut from. If the branch does not exist yet, cut it from the base, commit the docs on it and push it to `origin`. A later `smith loop run` adopts the existing branch by its number, so the docs are in what every task is cut from. Pass `--branch` to `loop list` and `loop run` alike to use a branch of your own naming.
