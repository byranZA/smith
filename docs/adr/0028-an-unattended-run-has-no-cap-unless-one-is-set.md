# An unattended run has no cap unless one is set

> Reverses the default of [#248](https://github.com/byranZA/smith/issues/248), where `--max-iterations` defaulted to 10, and gives the limits the repo file home [ADR-0012](./0012-the-repo-file.md) left for them.

## Context

An operator who starts `smith loop run` and walks away expects it to work the spec to completion. With a default cap of 10 agent runs, a spec of more than a handful of tasks stopped `cap reached` with its review deferred, though the operator never asked for a limit and the settings block never showed one ([#755](https://github.com/byranZA/smith/issues/755)).

## Decision

**A run has no cap unless `--max-iterations` or the repo file's `limits.agent_runs` sets one.** The attempts limit stays at two unless `--max-attempts` or `limits.task_attempts` sets it. A flag beats the repo file, which beats the default, and `--max-iterations 0` lifts a repo file's cap for one run. The settings block always shows both limits and where each came from.

- **What still bounds a run.** Each task, and the review, gets a bounded number of attempts; the review runs once per run; and a run whose attempts are all agent failures stops as `agent broken` ([ADR-0020](./0020-the-loop-stops-on-a-broken-agent.md)). A run on a box is the same verb, so it takes the same default ([ADR-0008](./0008-smith-runs-on-the-box.md)).
- **The accepted risk.** A spec whose agents keep filing new tasks never runs out of work, and only a cap, Ctrl-C or `session stop` ends it. A separate guard against that would be a cap under another name, so none is added.

## Considered options

- **Keep a default cap and show it.** Visible, but the run still stops short of what the operator expected when they set nothing.
- **A larger default cap.** Any fixed number is wrong for some spec, and it still stops a run nobody asked to stop.
- **The flag alone.** It must be repeated on every run, so a repo cannot say once how its runs are bounded.
