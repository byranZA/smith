# The loop posts each attempt's report on its task

> Amends [ADR-0016](./0016-the-loop-comments-on-a-task-it-skips.md), under which the loop commented on a task only when it skipped it. Takes its file pattern from [ADR-0025](./0025-the-loop-files-the-reviews-findings.md).

## Context

When an agent run ended, nothing durable said what it did. An unattended run's final message went to the operator's terminal and was lost, and the task kept only the agent's own `Done in <short sha>.` close comment, or a `WIP:` note if the agent wrote one. An interactive run left even less. A retrospective of what agents produced, and of where the task, the prompt, the repo or the machine got in their way, had nothing to read, and failed attempts, often the most useful to review, were lost entirely ([#774](https://github.com/byranZA/smith/issues/774), [#776](https://github.com/byranZA/smith/issues/776)).

## Decision

**Every attempt at a task, unattended or interactive, ends with the agent writing an attempt report to a file smith names, and the loop posts it as a comment on the task's issue.**

- **The file** is at a path the loop passes in `{{REPORT_FILE}}`, under `.git/smith/`, removed before each attempt and read after the agent exits, as the review's findings file is. The placeholder is optional: a repo prompt without it gets no reports.
- **The report has two parts.** Notes about the task, for whoever picks it up or reviews it: Done, Deviations / skipped, Left open (only when the task is not closed) and For review. Notes about the agent's environment, for whoever maintains the repo and its setup: Friction and Recommendations, each "None" when there is nothing to say.
- **The loop writes the header** and states its facts rather than taking the agent's word: that the comment is from smith, the attempt (N of M, or an interactive run), how it ended (`closed`, `left open`, `killed`, `agent failed`), the commit a closed task ended on, the preset, and the attempt's duration.
- **The agent no longer comments on the task.** It closes with no comment, and writes no `WIP:` note: the report's Left open section replaces it, and a retry reads it from the issue as before.
- **It is posted once the attempt's result is known**, before any merge, push or skip comment. A report too long for a GitHub comment is cut to its tail, behind a marker.
- **No report, or a failed read or comment write, is a warning, and the run carries on**, as for the other tracker writes. An interrupted attempt posts nothing.
- **The report is posted as written.** smith is credential-agnostic and holds no list of values to redact; the agent could already post anything with `gh`.
- **Nothing reads the report back** but a retry's agent. For the loop, like the labels and the skip comment, it is a signal for people.

## Considered options

- **Teeing the agent's stdout and posting its final message**, as #774 proposed. Rejected: an interactive run's stdout is the agent's own terminal interface, with no clean message to capture, and an unattended run's final message is whatever the agent said last.
- **Keeping the agent's `Done in <sha>.` close comment beside the report.** Rejected: two comments per close, and the sha is the agent's claim where the loop can read it itself.
- **A comment for every attempt, report or not.** Rejected: an agent that cannot run would fill each task with empty comments, and the stop report already covers it ([ADR-0020](./0020-the-loop-stops-on-a-broken-agent.md)).
- **A repo-file switch to turn reports off.** Not added: leaving the placeholder out of a repo's own prompt already does it.

## Consequences

- Each attempt adds a comment, so a task tried across several runs collects one report per attempt; nothing edits or deletes an earlier one.
- An ejected loop prompt keeps working but posts no reports until it adds `{{REPORT_FILE}}` and asks for the report.
- The review agent's final message stays unposted, and the full activity stream stays unkept; that belongs with [#612](https://github.com/byranZA/smith/issues/612).
