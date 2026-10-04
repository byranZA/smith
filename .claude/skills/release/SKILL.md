---
name: release
description: Cut a smith release — docs audit, release notes, version pins, tag, and post-release verification.
disable-model-invocation: true
---

# Release

A release is a promise that the docs describe the binary being shipped. The tag is the cheap part; the **audit** — every change since the last stable release traced to the doc that describes it — is the work. The audit ends in a record the tag step reads, so the tag waits on a finished audit.

## Vocabulary

- **Target** — the version being released, e.g. `v0.2.0`. A release candidate (`v0.2.0-rc.1`) shares its target's files; it is a rehearsal of that target, not a version of its own.
- **Range** — `<last stable tag>..HEAD`. Stable means no `-rc` suffix (`git tag --sort=-v:refname | grep -v -- -rc | head -1`). Every candidate and the final release of one target audit the same range, so a later candidate extends the record rather than starting a new one.
- **Record** — `docs/releases/<target>-audit.md`, one row per PR in the range.
- **Notes** — `docs/releases/<target>.md`, the hand-written release body.
- **Pins** — the literal version in the install docs: every `v0.x.y` and `0.x.y` in `README.md` and `docs/install.md`. Pins move only on a stable release, so the install path never points at a pre-release.

## Steps

### 1. Fix the target

Take the tag from the invocation argument; ask for it if none was given. Decide: is this the target's first candidate, a later candidate, or the stable release? Read the existing record and notes if they exist.

Branch `release/<target>` off an up-to-date `main`. Every file this skill writes lands on that branch and reaches `main` through a PR, like any other change.

**Done when** the target, the kind of release, the range, and the branch are settled.

### 2. Audit

List the range's PRs from the merge commits (`git log --format='%s' <range>` — each subject ends in `(#N)`; read a PR with `gh pr view N`). Add a record row for each PR not already in the record. For each row, read the diff, not only the title, and decide:

- **user-visible** — it changes a command, flag, output, config key, file on disk, provisioning behaviour, or a refusal. Name the doc section that describes it, then read that section against the merged behaviour. A section that merely mentions the feature is not coverage; the section has to say what the binary now does.
- **internal** — refactor, test, CI, or standards work with no observable effect. Say why in one phrase.

Write a fix for every **gap** (a user-visible row with no accurate section) on the release branch, and mark the row `fixed`. A gap too large for the release is the user's call: ask, and if they defer it, open an issue and put its number in the row.

Then sweep the docs as a whole, because a PR can make a page wrong that its diff never touched:

- **Commands** — `go run ./cmd/smith <cmd> --help` for every command a doc names; every flag, verb, and argument in the docs exists and matches.
- **Counted claims** — grep for number words (`two`…`ten`) beside nouns like verbs, stages, steps, files; recount each against the code.
- **Index** — every row in `docs/README.md` still describes its page, and every page in `docs/` has a row.
- **Examples** — `go run ./cmd/smith blueprint check docs/examples/blueprints/acme.yaml` passes; the adapter and preferences examples use only keys the code reads.
- **Domain** — a new term in the range appears in `CONTEXT.md`; a decision that constrains future work has an ADR.

Record each sweep finding as a row of its own (`sweep` in the PR column). The record is one table:

```markdown
# <target> docs audit

Range: `<last stable tag>..<commit>`

| PR | Title | Kind | Covered by | Status |
| --- | --- | --- | --- | --- |
| #215 | smith init scaffolds the config home | user-visible | docs/blueprints.md § The config home | accurate |
| #238 | shipped-script lifecycle as one module | internal | refactor, no behaviour change | — |
| sweep | docs/README.md says "five session verbs" | user-visible | docs/README.md | fixed |
```

Status is one of `accurate`, `fixed`, `deferred #N`, or `—` for internal rows. When a later candidate extends the record, update the range's end commit.

**Done when** every PR in the range has a row, every user-visible row names a section you read and found accurate or is `fixed` or deferred to an issue, and every sweep check has run.

### 3. Notes

Write `docs/releases/<target>.md` for someone already running the previous stable release:

```markdown
# smith <target>

<Two or three sentences: what this release makes possible.>

## Upgrading

<What the reader must do: always `smith machine upgrade <box>` for each box, since on-box smith refuses a version mismatch; plus any config key, inventory schema, or blueprint change that needs action. Omit nothing a user would trip on.>

## What's new

<One bullet per user-visible change, linking the doc section from the record.>

## Fixes

<One bullet per user-visible fix.>
```

Group by what the user does, not by PR. Internal rows stay out. GoReleaser appends the commit changelog below these notes, so the notes carry the meaning and the changelog carries the detail.

**Done when** every user-visible row in the record appears in the notes, and the upgrading section names every action the record implies.

### 4. Pins — stable release only

Replace every pin in `README.md` and `docs/install.md` with the target (both the `v`-prefixed tag and the bare version in archive filenames). `grep -rn '<previous stable version>' README.md docs/` returns nothing outside `docs/releases/` when finished.

A candidate leaves the pins alone and skips this step.

### 5. Land

Run `make check`. Open a PR from the release branch with the record, notes, doc fixes, and pins. Hand it to the user to merge; once merged, `main` must be green in CI (`gh run list --branch main --limit 1`).

**Done when** the PR is merged and the CI run on the merge commit has passed.

### 6. Tag

Re-read the record: every row is resolved. Show the user the tag, the commit it lands on, and whether GoReleaser will mark it a pre-release, and wait for an explicit yes — a pushed tag publishes binaries.

```sh
git switch main && git pull --ff-only
git tag -a <tag> -m "smith <tag>"
git push origin <tag>
```

Watch the release workflow (`gh run watch`). When it finishes, put the notes at the top of the release body, keeping the generated changelog below them:

```sh
{ cat docs/releases/<target>.md; echo; gh release view <tag> --json body -q .body; } > "$TMPDIR/notes.md"
gh release edit <tag> --notes-file "$TMPDIR/notes.md"
```

### 7. Verify

Prove the published release is the one the docs describe:

- Run the install one-liner from `docs/install.md` for this machine's platform (for a candidate, substitute the candidate's tag); the binary's `smith version` prints the target.
- `shasum -a 256 -c checksums.txt --ignore-missing` against the downloaded archive passes.
- `gh attestation verify <archive> --repo byranZA/smith` passes.
- `gh release view <tag>` shows the notes, the five archives plus `checksums.txt`, and the right pre-release flag.

**Done when** each check passed — report each one's result to the user. A failure is reported, not retried around: a bad release is fixed with a new tag, never by moving one.
