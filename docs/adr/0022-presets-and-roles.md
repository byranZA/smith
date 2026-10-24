# Agent settings are named presets, chosen by role and by task

> Amends [ADR-0012](./0012-the-repo-file.md), which gave the repo file one flat `agent`, `model` and `effort`, each overridable on its own.

## Context

A model belongs to one agent, but the repo file held the agent and the model as separate fields, resolved one at a time. This repo sets `agent: claude` and `model: opus`, so `smith loop run --agent codex` handed `opus` to codex verbatim, and the #520 box check had to pass `--model` by hand. Effort is different: it is smith's own scale, which each adapter translates, so it carries across agents.

One setting also cannot describe several jobs. A hard task may want a stronger model than the rest of its spec, and a review step, when one exists, may want a different agent from the work.

## Decision

**A preset is a named agent, model and effort, chosen whole. A repo defines its own presets in the repo file, and a role or a task picks one by name.**

```yaml
presets:
  opus:  { agent: claude, model: opus,        effort: medium }
  codex: { agent: codex,  model: gpt-6.1-sol, effort: high }
roles:
  work: opus
```

- **The repo defines presets, and smith defines roles.** A repo may define any number of presets under any names. A role is a job smith hands an agent, and smith's code decides when each one runs, so a repo only says which preset a role uses. `work` is the only role today, and the interactive loop uses it too, because being attached to a terminal is a way of running a role, not a role of its own.
- **A task names a preset with a `preset:<name>` label.** The task's author knows whether it is hard, and the label is visible and easy to change in the tracker. A task names a preset and nothing else, so a model string never sits unvalidated in an issue. Today the label overrides the `work` role only. Whether it also applies to `review` is decided when `review` exists.
- **Precedence is run flag > task's preset > role's preset > built-in default.** The flag wins so that an operator can move a whole run elsewhere, for example when one agent is down. The built-in default is `{agent: claude}` with model and effort unset. It applies when the repo file sets no preset for `work`, even if it defines presets for tasks to name.
- **Flags:**
  - `--preset <name>` picks one of the repo file's presets.
  - `--agent X [--model Y]` builds a one-off preset that never inherits the repo file's model.
  - `--model` alone replaces the model of the agent the run would otherwise use.
  - `--effort` overrides the effort of any of these.
  - Any of `--preset`, `--agent` or `--model` pins the whole run to one preset, over every task's label, so a run-wide model never reaches a task on another agent. `--effort` alone keeps each task's preset and changes only its effort, because effort carries across agents.
- **A task's preset that cannot be resolved stops the run before any agent runs.** The loop checks every task's labels when it reads the execution, as it does with a tracker it cannot reach, and stops on a preset the repo file does not define or a task carrying two. Either is a configuration error, not a task failure, so the task is not skipped to a human.
- **Every preset is validated when the repo file loads**, used or not: its agent must be known, its effort on the scale, and its name lowercase letters, digits and hyphens, so that `preset:<name>` is a clean label.
- **Presets live in the repo file only.** ADR-0012's reserved preferences slot stays reserved. A later preferences layer could add presets that merge by name. The blueprint stays responsible for the box.
- **The flat `agent`, `model` and `effort` keys are rejected** with an error that shows the new shape. `smith repo init` writes presets.

## Considered options

- **A model per agent** (`agents: {claude: {model: opus}}` beside `agent:`). Rejected: it fixes the stale model but cannot give two jobs different settings for the same agent, and a task would have nothing to name.
- **A map from task number to setting in the repo file.** Rejected: issue numbers are short-lived, the committed file would fill with entries for closed tasks, and a task's description would be split across two places.
- **A task label carrying its own agent, model and effort.** Rejected: it brings back the stale-model problem and puts model strings where nothing validates them.
- **A task's preset winning over the run flag.** Rejected: the operator would have no single switch to move a run off a broken agent.
- **Keeping the flat keys as shorthand for an unnamed `work` preset.** Rejected: smith is pre-1.0 with few repo files, and two ways to say one thing makes the strict validator harder to explain.
- **A stopgap that drops the repo file's model when `--agent` changes the agent.** Not built: `--agent` building a one-off preset fixes the same bug.

## Consequences

- This repo's `.smith/repo.yaml` must move to presets in its own HITL task before a smith carrying this change runs the loop on it.
- `/spec-to-tasks` and the people writing tasks gain a lever: a task that needs a stronger model gets a `preset:` label instead of a different run.
- Adding `review` means adding a role, not a new settings shape.
