# Working on smith

For anyone working *in* the repository — the author and the agents smith runs.
Smith is not accepting outside pull requests yet; see
[CONTRIBUTING.md](../CONTRIBUTING.md).

This page covers setting a machine up, the quality gate, and how changes land.
How to write Go here is the `coding-standards` skill
(`.claude/skills/coding-standards/`); why smith is shaped the way it is, and how
work is tracked, is [AGENTS.md](../AGENTS.md).

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

## Branches and pull requests

- Branch off `main`; keep one concern per branch.
- `make check` green, plus `make race` if the change touches concurrency.
- The PR body says what changed and why, and names the issue it closes.
- New behaviour lands with tests, and with its user-facing documentation updated
  in the same change — [`docs/`](./README.md) for behaviour, an ADR for a
  decision that constrains future work.

## Documentation layout

Keep the README thin: what smith is, and enough of each capability to know
whether you want it. Detail belongs in [`docs/`](./README.md), one page per
area, linked from the README section that introduces it.
