# Contributing to smith

> **Smith is not accepting outside contributions yet.** It is early-development
> software being built for personal use, and the design is still moving too fast
> for an outside change to land against a stable target. Pull requests from
> outside the project will be closed unread — not because the work is unwelcome,
> but because there is nothing yet for it to be reviewed against.
>
> **Bug reports and questions are welcome** as
> [issues](https://github.com/byranZA/smith/issues). If you are using smith and
> something is broken or unclear, that is genuinely useful now, and it is the
> kind of feedback that will decide when this door opens.

This file stays here because it is accurate for anyone working *in* the
repository — that is, the author and the agents smith runs. It is the **human**
entry point: how to set a machine up, what the quality gate is, and where the
standards live. It does not restate the standards themselves.

The bar is "the gate is green and the change is explained", not a formal
process.

## Set up a fresh machine

You need Go 1.26+ (matching `go.mod`) and `git`. Clone, then install the dev
tools:

```bash
make tools-install                          # golangci-lint, govulncheck, goimports
export PATH="$PATH:$(go env GOPATH)/bin"    # where they land — add to your shell profile
```

`tools-install` pins each tool to the version CI uses, read from the `Makefile`.
CI reads the golangci-lint pin back out of that same `Makefile`, so the linter
that gates a pull request is the one you just installed — one pin, not two
floating ones.

> **Skipping the tools does not fail the gate — it hides it.** `make lint` and
> `make vuln` print a note and succeed when their tool is absent. A machine
> without them runs `make check`, prints two skips, and **reports green having
> linted nothing**; CI then fails on lint errors your local gate never looked
> for. That is not hypothetical — it is how a `wrapcheck` failure reached CI on
> [#92](https://github.com/byranZA/smith/issues/92) (PR
> [#174](https://github.com/byranZA/smith/pull/174)). If you genuinely cannot
> install the tools, run `gofmt -l .`, `go vet ./...` and `go test ./...` at
> minimum, and expect CI to be stricter than you were.

Confirm the tools are visible before you trust a green run:

```bash
command -v golangci-lint govulncheck goimports
```

## The gate

`make check` is what CI runs. Run it before you push:

```bash
make check     # fmt-check + vet + lint + test
```

Two things it does not cover:

```bash
make race      # tests under the race detector — required for anything concurrent
make vuln      # govulncheck — CI runs this as a separate gate
```

The rest of the targets — `build`, `test`, `cover`, `fmt`, `tidy`, `snapshot` —
are listed with a one-line description each in the `Makefile`; `make tools`
prints what `tools-install` would install without installing it.

## Standards

- **How to write Go here** — the `coding-standards` skill
  (`.claude/skills/coding-standards/`). Structure, naming, doc comments, error
  handling, testing, and the targeted `go` invocations for narrowing a run.
- **Why smith is shaped the way it is, and its boundaries** —
  [AGENTS.md](./AGENTS.md), with the decision records in [`docs/adr/`](./docs/adr/)
  and the domain model in [CONTEXT.md](./CONTEXT.md).

Read them rather than inferring the conventions from nearby code; where the two
disagree, the standards win and the code is wrong.

## How work is tracked

Issues live in **GitHub Issues on `byranZA/smith`**, managed with the `gh` CLI.
Each issue carries one of five triage labels — see
[docs/agents/issue-tracker.md](./docs/agents/issue-tracker.md) and
[docs/agents/triage-labels.md](./docs/agents/triage-labels.md).

**External pull requests are not a triage surface**: the tracker is where work is
planned and picked up, and a PR that arrives without an issue behind it has
nowhere to be triaged to. This is the mechanical reason behind the note at the
top of this file — an issue has somewhere to go, a PR does not.

## Branches and pull requests

These apply to work inside the repository, per the note at the top.

- Branch off `main`; keep one concern per branch.
- `make check` green, plus `make race` if the change touches concurrency.
- The PR body says what changed and why, and names the issue it closes.
- New behaviour lands with tests, and with its user-facing documentation updated
  in the same change — [`docs/`](./docs/) for behaviour, an ADR for a decision
  that constrains future work.

## Documentation layout

Keep the README thin: what smith is, and enough of each capability to know
whether you want it. Detail belongs in [`docs/`](./docs/README.md), one page per
area, linked from the README section that introduces it.
