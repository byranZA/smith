# The loop

The **loop** works a spec's tasks with a coding agent, one task per agent run. It asks the tracker which task is next, hands exactly that task to an agent, and reads the tracker again afterwards to see whether the agent closed it. The loop decides when it is done, not the agent.

```text
smith loop list <spec>
smith loop run  <spec> [--agent <name>] [--model <name>] [--effort low|medium|high]
                       [--max-iterations <n>] [--max-attempts <n>] [--interactive]
smith loop prompt
smith repo init [--agent <name>] [--model <name>] [--effort low|medium|high]
```

Every verb runs in the repo it is invoked from: the loop works the current working tree on the current branch. It never commits, pushes or switches branch itself. The prompt asks the agent to commit, so a failed attempt's changes are still in the tree for the next attempt.

## What the loop reads from GitHub

The tracker is GitHub Issues, reached through `gh`, so `gh` must be installed and authenticated.

- **A spec** is an issue labelled `spec`, named as `42`, `#42` or its issue URL. A URL for an issue in a different repo from the one `gh` sees in the working directory is refused.
- **Its tasks** are its sub-issues. The ones its `## Tasks` checklist lists come first, in that order, then any other sub-issue in the order GitHub holds them.
- **Who a task is for** comes from its labels: `ready-for-agent` tasks go to the agent, and `ready-for-human` tasks are never handed out. A task with both labels is kept for the human. A task with neither is not ready yet, and a sub-issue labelled `spec` is a nested spec, never a task.
- **Blockers** are a task's GitHub "blocked by" links, plus any issue named in its `## Blocked by` section. A task is **available** when it is open, for an agent, and every blocker is closed.

The loop hands out the first available task in the spec's order. It reads the tracker again before every choice, so a task filed or unblocked mid-run is seen.

## `smith loop list`

Shows the settings a run would use, the next task, and what remains, without running anything:

```text
agent:  claude (repo file)
model:  opus (repo file)
effort: high (repo file)
spec #244 Spec: the loop — smith loop and the repo file
next: #253 Loop: smith runs its own loop — .smith/ config, prompt and docs
  blocked: #254 (by #253)
  waiting on a human: #255
  closed: #245, #246, #247
```

Each setting names where its value came from. A kind of task with nothing in it is left out.

## `smith loop run`

Prints the same settings block as `list`, then works the spec unattended until no task is available. As it goes, it writes `smith: handing #N <title> to the agent` to stderr before each agent run, and `smith: the agent on #N ended with a failure: …` when one fails. At the end it reports why it stopped:

- **`spec #N complete`**: every task is closed. Exit 0.
- **No task is available for an agent**: what remains is blocked, for a human, or not ready. Exit 1.
- **The agent left a task open on every attempt**: see attempts below. Exit 1.
- **Reached the cap of N agent runs**: see iterations below. Exit 1.

Two limits bound a run so it can be left alone:

- **`--max-attempts`** (default 2): a task the agent leaves open is handed out again until it has had this many runs, then skipped for the rest of the run. A skipped task stays open on GitHub, usually with the agent's WIP note on it.
- **`--max-iterations`** (default 10): the most agent runs the whole loop makes.

An agent that exits with a failure is reported and the loop carries on: whether the task is done is the tracker's call, not the exit code's. Ctrl-C stops the running agent and starts no further task.

### `--interactive`

Hands the next available task to one agent run attached to your terminal, then stops. The prompt gains a note asking the agent to check its plan with you, ask rather than guess, and show you the result before it commits and closes the task. Approvals are the agent's own, so you answer them as you would in any session. The limits play no part. It reports `task #N closed` and exits 0, or `task #N still open` and exits 1. With no task available it runs no agent, prints the same stop report as `run`, and exits 0 only if the spec is complete.

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
```

`smith repo init` writes a commented starter, filling in any value you give as a flag, and reports `created <path>`. It never overwrites an existing file: it reports `left alone <path>` instead. Outside a git repo it refuses: `must be run inside a git repo`.

Each setting resolves on its own, most specific first:

1. A flag on `smith loop run`, for that run only.
2. The repo file.
3. The built-in default: `claude`, and the agent's own model and effort.

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
