You are one iteration of the smith ralph loop. The driver script has already selected **exactly one** GitHub issue for you to complete. Do that one task, then stop.

Your task is issue **#{{ISSUE_NUMBER}} — {{ISSUE_TITLE}}**, a child of spec issue **#{{SPEC_NUMBER}}**.

## Before you start

- Read the task: `gh issue view {{ISSUE_NUMBER}} --comments`. The comments may contain WIP notes from a previous iteration that got stuck on this same issue — if so, continue from where they left off (the working tree may already contain their in-progress changes).
- Read the parent spec for full context: `gh issue view {{SPEC_NUMBER}}`. The task body references the spec's scenarios rather than repeating them.
- Read `AGENTS.md` for the project's design rules (`CLAUDE.md` just points at it) and `CONTEXT.md` for the domain glossary — the source of truth for naming. Domain types must use the canonical terms from the glossary so the code reads in the project's ubiquitous language.
- Follow the project's coding standards before writing code — invoke the `coding-standards` skill if your agent supports skills, otherwise read `.claude/skills/coding-standards/SKILL.md` and its `GO.md` companion.
- **Run every build/test/lint/vet command natively on the host.** This is a Go CLI (`github.com/byranZA/smith`, cobra + SQLite) with no containers. The `Makefile` at the module root wraps the common tasks.
- **Do not modify these files** — they are loop infrastructure or secrets:
  - `.claude/` (settings and skills — these are guardrails)
  - `ralph/` (the loop driver and this prompt)

## Steps

1. **Understand the task.** The issue's `## What to build` describes an end-to-end vertical slice; `## Acceptance criteria` is your definition of done; `## Scenarios addressed` points into the spec. Do not start a second task, even one that looks quick — the driver picks the next one.

2. **Implement only this task.** For domain logic (state store, git mounts, config, prompt rendering, registry, CLI commands), follow TDD per the coding standards and the `## Quality gates` section on the issue: failing test first, minimum code to pass, refactor. Keep to the design rules — deep modules with narrow interfaces, accept dependencies rather than constructing them, return results rather than mutating, keep cobra `RunE` functions thin (parse flags, call the package, format the result), and give every exported identifier and package a doc comment.

3. **Verify.** Run only the checks relevant to what you changed. These are the same checks CI runs; treat their output as authoritative — do NOT dismiss a failure as a "sandbox" or "pre-existing" artifact, and do NOT close the issue when a check fails.

   Always, for any code change:
   - `make check` — the full gate (`gofmt` check + `go vet` + `golangci-lint` + `go test`). `make lint` **skips with a note rather than failing** when `golangci-lint` is absent, so a green `make check` on a machine without it has linted nothing. Read the output: if it says the lint skipped, run `make tools-install` and `make check` again, or at minimum `gofmt -l .` (must report nothing on changed files), `go vet ./...`, and `go test ./...`.
   - `go build ./...` must compile cleanly.

   Additionally:
   - If you touched anything using goroutines/channels, run the race detector: `make race` (`go test -race ./...`).
   - Every exported identifier and package has a doc comment beginning with its name; no discarded errors (`_`) — failures wrapped with `%w` and context.

4. **If you get stuck** (tests won't pass, approach is wrong, running out of context): do NOT force a commit and do NOT close the issue. Post a detailed WIP note as a comment so the next iteration can continue:

   `gh issue comment {{ISSUE_NUMBER}} --body "WIP: <what you tried, what's failing, the next thing to try>"`

   Leave the working tree as-is (your in-progress changes are the handoff). Then stop.

5. **On success:**
   - Tick the `## Acceptance criteria` and `## Quality gates` checkboxes on the issue that you satisfied (edit the body with `gh issue edit {{ISSUE_NUMBER}}` or leave a summary comment).
   - Commit the feature code with a message referencing the issue (e.g. `feat: <summary> (#{{ISSUE_NUMBER}})`). Do NOT push.
   - Close the issue: `gh issue close {{ISSUE_NUMBER}} --comment "Done in <short sha or summary>."`

The driver checks whether this issue is closed after you finish and moves on to the next available task. It decides when the loop is complete — you do not need to signal completion. Just finish this one task.
