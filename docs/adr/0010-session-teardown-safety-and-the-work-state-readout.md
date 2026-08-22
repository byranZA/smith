# `session rm` refuses a dirty worktree or a live session — and nothing else

## Context

On an ephemeral box, pushed is the only durable state, so the instinct is that reclaiming a
worktree can vaporise work that exists nowhere else. That instinct produced this question as
"`rm` must refuse unpushed commits" — and the premise turned out to be **wrong**, in a way that
*is* the rule.

The session verbs preserve the branch, and the managed repo lives at
`~/workspace/<repo>/repo.git` as a **bare** repo with worktrees beside it. So `git worktree
remove` deletes a working *directory* while the branch ref and every commit on it stay in the bare
repo, and `session start` on that branch re-materialises them. Placed files come back from the
blueprint's re-converge. Nothing else was ever there.

`rm` is therefore **reversible except for uncommitted changes** — and the gate covers precisely
the non-reversible part. That is the entire safety story, and every sub-question falls out of it.

The same per-worktree predicate has a second consumer: with no `destroy` verb in v1, a `session
list` readout is what an operator reads before tearing a box down at the provider by hand. This
ADR covers both halves.

## Decision

**`session rm` refuses on a dirty worktree or a live session, and on nothing else. One `--force`
covers both. Every refusal prints what would be lost and exits non-zero.**

### What gates

- **Dirty worktree** — `git status --porcelain` non-empty: tracked modifications, staged changes,
  untracked files; ignored files excluded. Deliberately the same set `git worktree remove` itself
  refuses on, so smith's rule matches git's and `--force` maps onto `git worktree remove --force`.
- **Live session** — the `tmux has-session` probe.

**Unpushed commits do not gate.** `rm` *reports* them (``branch `spec-42` kept, 3 commits not on
any remote``) and proceeds. Gating on them would fire on nearly every session — the normal state
of an in-progress branch is unpushed — and a check that always fires trains `--force` into muscle
memory, which is how you lose the dirty-worktree gate that actually matters.

### Placed files must not trip the untracked check

Placements write `.env`-shaped files *into* the worktree and re-converge them on every `start`. A
placed file that is not ignored reads as untracked, so `rm` would refuse on a file smith itself
wrote and can rewrite from the blueprint.

**The check subtracts smith's own placement `to:` list** for that repo. That list is the source of
truth, held in memory, correct regardless of what any ignore file says — correctness must not
depend on a file smith does not control.

Separately and for a different consumer, `machine setup` converges the placement paths into
**`repo.git/info/exclude`** as a marked managed block (check-before-change), so an operator who
SSHes in and runs `git status` does not see smith's `.env` as noise. Worktrees share the common git
dir, so one write covers them all. The safety check does not depend on this write; it only stops
the human's view from lying.

**Never `.gitignore`.** It is tracked, so writing to it creates the very tracked modification that
makes `rm` refuse — smith generating its own false positive. It is the operator's repo content, and
an agent in that worktree would commit and push smith's edit into their real repository. And
[ADR-0008](./0008-smith-runs-on-the-box.md) fixed smith's git surface at `worktree add`/`remove`;
editing repo files is past that line.

### The refusals are separate messages

A running driver is a process you would be killing — a different kind of loss from a dirty file —
and collapsing them makes the output vague exactly where it must be precise:

- **live**: ``session `smith/<name>` is running — `smith session stop <name>` first``
- **dirty**: the enumeration — modified, staged, and untracked counts (placements excluded), plus
  the unpushed-commit count as information, plus the exact `--force` command.

Refusing without saying what would be lost is the failure mode this decision exists to prevent.

### No TTY prompt

[ADR-0008](./0008-smith-runs-on-the-box.md) prompts on version-skew refusal when stdin and stdout
are both a TTY. **`rm` does not follow that precedent.** Skew is converge-then-continue with a
safe answer; this is a delete with no undo, and a `y/N` at a delete prompt is answered reflexively.
Print what dies, print the `--force` command, exit non-zero — always, identically under a human and
under an AFK loop.

### `stop` needs no rule

`stop` keeps the worktree and the branch; only the tmux session ends, which is the normal
completion signal anyway. It is safe by construction. Stated explicitly so nobody later adds a
symmetric gate "for consistency".

### The predicate is defined once, with two consumers

`rm` gates on it; `session list` displays it. **Unpushed means commits not reachable from any
remote-tracking ref** — `git rev-list --count <branch> --not --remotes` — *not* `@{u}..HEAD`. A
branch smith cut that the agent has not pushed has no upstream, so the upstream form errors on the
common case instead of reporting; and the remotes form correctly reports zero when the work reached
the remote under a different name or was merged into something already pushed. Safe-on-a-remote is
the only thing the column claims.

### The bare repos are the worktree registry

`list` walks `~/workspace/*/repo.git` and runs `git worktree list --porcelain` per repo — **not**
the filesystem under `worktrees/`. A bare repo *is* a worktree registry: it hands back the
**unsanitized** branch (the directory name is sanitized, i.e. lossy by construction) and it draws
the line between a session and debris. A directory under `worktrees/` that git does not know about
is not a session, and inventing a row for it would make `list` disagree with `rm`.

### `session list` shows the whole predicate and nothing else

Columns: **`NAME  STATE  DIRTY  UNPUSHED`** — four, always, no flag, nothing hidden. Anything
behind a flag means the pre-teardown read needs the flag to be worth reading. `REPO`/`BRANCH` do
not earn columns; the name already carries both.

**`NAME` is the sanitized session name** — the same string `rm`/`attach`/`stop` take. The column's
job is to be pasted into the next command, so a name that does not round-trip is the failure that
matters. The true branch appears in no default output and stays load-bearing *internally*, as what
`--not --remotes` counts against and what proves a directory is debris.

**Flat, sorted by repo then branch. Never grouped, never paginated, never truncated.** Group
headers break `grep`, `awk`, and copy-paste of a name — the three things an operator does with
fifty rows. Scale is answered by **filters, not layout**: `--repo <name>`, `--live`, `--stopped`.

**A summary line is always printed**: `7 sessions · 2 dirty · 3 with unpushed commits`, or
`7 sessions · all clean`. The reassuring read is the one that matters before destroying a box by
hand, and it only reassures if it is unconditional.

**`list --names`** — one name per line, no header, no summary, no columns — is what makes the
sweep composable: `smith session rm $(smith session list --stopped --names)` reads as intended and
adds no vocabulary.

### Batch `rm` is N explicit names

Filters live on `list` only. A `--stopped` on `rm` makes the target set implicit, which is exactly
the property this decision avoids given that a delete has no undo and deliberately ships no
confirmation prompt.

**One refusal kills the whole batch**: gate every name first, refuse all, enumerate every
offender. Partial deletion is the state nobody can reason about afterwards, and all-or-nothing
keeps `--force` a deliberate second command rather than a half-applied one.

## Considered options

- **Gating on unpushed commits** — rejected; see above. The exposure of unpushed work is real but
  belongs to *the box dying*, not to `rm`.
- **A `y/N` confirmation prompt** — rejected. Reflexive at a delete, and it would behave
  differently under an AFK loop than under a human.
- **Writing placement paths into `.gitignore`** — rejected. Tracked file, operator's content,
  self-inflicted dirty state, and past smith's git surface.
- **A non-zero exit from `list` when something is unpushed** — rejected. It conflates *command
  failed* with *data has a property*, and v1 ships no `destroy` verb to consume it. Re-entry
  belongs to whoever adds one.
- **Grouping `list` by repo** — rejected: headers break the three things operators actually do
  with the output.
- **`--json` output** — deferred to whichever ticket first has a real consumer, for `session list`
  and `machine list` at once. Two list verbs with different structured formats is the outcome
  nobody wants.
- **An automatic reaper** (pruning finished worktrees by age, merged-ness, or pushed-ness) — ruled
  out of scope. It needs a policy nobody has a basis to pick yet, and something resident to run
  it; worse, it makes smith destroy things unasked, which has to clear a far higher bar than the
  manual verb this ADR just spent itself gating. The cheap 80% is batch `rm` plus `list --names`.

## Consequences

- **The check runs on the box.** Every verb executes there
  ([ADR-0008](./0008-smith-runs-on-the-box.md)), so on-box smith inspects the worktree it is
  standing next to; local smith relays and renders.
- **This is not sufficient for box `destroy`.** It is the **single-worktree** case, where smith
  already knows which worktree it is reclaiming. `destroy` must enumerate *every* managed worktree
  and be right about all of them, and it inherits an async-delete complication — `list` still
  returns a box after a successful delete
  ([ADR-0007](./0007-the-provider-adapter-is-data.md)). Whoever adds `destroy` settles teardown
  safety fresh.
- **v1 teardown is a three-step human process**: push everything you care about →
  `machine forget <name>` → destroy at the provider by hand, using the destroy reference the marker
  records. Step three is the genuinely destructive act and it happens outside smith. `session list`
  is what answers "am I about to lose anything?" before it.
- **A detached-HEAD worktree still lists** — it is a real worktree with a real tmux session — and
  `UNPUSHED` reads as `-`, because `--not --remotes` has no branch to count.
- **Cost stays trivial**: two git commands per worktree, no provider call, so `list` is instant and
  needs no cache.

See [issue #68](https://github.com/byranZA/smith/issues/68) and
[issue #76](https://github.com/byranZA/smith/issues/76).
