# Sessions

A **session** is one `tmux` session plus one git worktree of one repo on one
branch. It is the unit of parallel work on a box: a feature on one branch, a
review of someone else's on another, a long test run on a third — each with its
own worktree, its own shell, and its own long-running processes.

Five verbs manage them:

```
smith session start   [<box>] --repo <name> --branch <name> [--base <ref>] [--detach]
smith session attach  [<box>] <name> [--interact]
smith session list    [<box>] [--repo <name>] [--live|--stopped] [--names]
smith session stop    [<box>] <name>
smith session rm      [<box>] <name>... [--force]
```

The verbs live **on the box**. Naming a box relays the verb over SSH to the
smith installed there; naming none runs it here. So `smith session list dev`
from your laptop and `smith session list` after SSHing in are one
implementation of one verb, reached through one door.

A box is resolved the way it is everywhere else in smith: a value containing
`@` is an SSH target used as written, a value without one is looked up in the
[box inventory](./inventory.md). A box that has never had smith installed says
so and names the fix:

```
box smith@203.0.113.10 has no smith installed: `smith machine setup smith@203.0.113.10`
installs it, along with the rest of what the box is missing
```

## Names are derived, never chosen

The session name is `<repo>-<branch>`, sanitized to one path segment: every
character outside `[A-Za-z0-9._-]` becomes a dash. Its `tmux` session is
`smith/<name>`, and its worktree sits at
`<workspace>/<repo>/worktrees/<sanitized-branch>`, beside the bare
`<workspace>/<repo>/repo.git` it was cut from.

```
repo "smith", branch "main"           →  smith-main       (tmux: smith/smith-main)
repo "smith", branch "smith/spec-42"  →  smith-smith-spec-42  (tmux: smith/smith-smith-spec-42)
```

That name is the only string the other four verbs take, and the only identity
`list` prints — because the column exists to be pasted into the next command.

Two consequences worth stating:

- **The sanitizer is lossy, deliberately.** A branch is never recovered from a
  name; smith reads the true branch back from `git worktree list`. If two
  branches in one repo derive the same name, `start` refuses and names both
  branches rather than quietly handing you the wrong worktree.
- **The `smith/` namespace keeps your own `tmux` sessions out.** A session you
  started by hand and called `smith-main` is not `smith/smith-main`, so smith
  never lists it, attaches to it, or kills it.

## The working loop

Stand a session up and land in it:

```sh
smith session start dev --repo smith --branch spec-42
```

`start` cuts the branch, creates the worktree, converges the repo's declared
placements into it, launches `tmux`, and connects you **writable** — because
`start` is the verb of someone who is there to work. Add `--detach` to stand it
up and return instead, which is what you want when you are standing up five
sessions rather than sitting down at one:

```sh
smith session start dev --repo smith --branch spec-42 --detach
```

```
session smith-spec-42 is live on branch spec-42 of smith
```

Come back to it later to watch, or to type:

```sh
smith session attach dev smith-spec-42              # read-only
smith session attach dev smith-spec-42 --interact   # writable
```

Disconnecting does not end anything. A session ends when its root process
exits, or when you stop it:

```sh
smith session stop dev smith-spec-42
```

```
session smith-spec-42 is stopped; its worktree and branch are untouched
```

Read the box before you do anything irreversible:

```sh
smith session list dev
```

```
NAME             STATE    DIRTY  UNPUSHED
smith-main       live     -      0
smith-spec-42    stopped  dirty  3
web-hotfix       stopped  -      0

3 sessions · 1 dirty · 1 with unpushed commits
```

And reclaim what you are done with:

```sh
smith session rm dev web-hotfix
```

```
session web-hotfix removed; branch hotfix kept
```

## `start` is ensure-running, not create

One probe pair decides everything `start` does: does the worktree exist, and is
its `tmux` session live.

| worktree | tmux | what `start` does | placements |
| --- | --- | --- | --- |
| absent | — | fetch, cut the branch, `git worktree add`, launch `tmux` | converged |
| present | dead | launch `tmux` in the existing worktree | converged |
| present | live | connect you writable | **untouched** |

So running `start` twice is not an error, and there is no separate "resume"
verb. The third row is load-bearing: smith will not swap a file out from under
a running process.

**That is also how a changed blueprint lands: `stop`, then `start`.** A session
that is live is never re-converged underneath you. Stop it and start it again
and its placements are rewritten from the bytes `machine setup` staged on the
box.

A **write-once** placement is still not overwritten on the way through — that
is what write-once means, and resume does not make an exception of it.

### Cutting, fetching and `--base`

- A **new** branch is cut from `--base`, else from the base the blueprint
  declares for that repo, else from the repo's default branch.
- smith runs `git fetch` **before cutting**, and on no other path. A branch cut
  from a stale base is a merge-time failure that looks like anything but a
  session bug, and the fetch is also what keeps the unpushed count honest.
- `--base` applies **only when cutting**. Against a branch that already exists
  it is a refusal, not a silent no-op:

  ```
  branch "spec-42" of repo "smith" already exists, so start cannot base it on
  "release-2": start cuts a branch from a base, it never rebases or resets one
  ```

  Rebasing and resetting are yours to do inside the session.

`start` refuses a repo the blueprint does not declare, and a branch already
checked out in another worktree — naming the session holding it, since git
allows one worktree per branch.

## Attaching: two access levels, one `tmux` session

`attach` connects **read-only by default**; `--interact` connects writable.
Both are the same `tmux` session — observing an agent at work and taking over
from it differ by one flag, not by a different mechanism. `start` connects
writable, because you asked for a session to work in.

smith `exec`s into `tmux`, so `attach` never returns: what you are talking to
afterwards is `tmux` itself, with correct resize and signal handling and no
smith process in the middle copying bytes.

Attaching to a stopped session says how to resume it, rather than resuming it
for you:

```
session "smith-spec-42" is not running: resume it with
`smith session start --repo smith --branch spec-42`
```

### Read-only is advisory, not a permission boundary

`--interact` is the difference between `tmux attach -r` and `tmux attach`.
**Anyone who can SSH to the box as `smith` can run `tmux attach` themselves,
without `-r`, and type into any pane.** Read-only guards against *you* typing
into a pane you meant only to watch. It is not a permission model, it does not
partition operators, and it must not be relied on as though it did.

### Nothing is exposed

Attaching adds **no listener, no port, no tunnel and no new credential**. It
rides the SSH door bootstrap already built:

```
ssh -t <box> /usr/local/bin/smith --relayed-from <version> session attach <name> [--interact]
```

This is byte-identical under `--access=public` and `--access=tailscale`. The
two access modes differ in how you reach the SSH door — a public port 22 versus
Tailscale SSH on the tailnet — and in nothing else. A session is never
published, and there is no browser terminal in v1.

## Ending a session: `stop` and `rm`

`stop` ends the `tmux` session and leaves the worktree and the branch exactly
where they are. It has no `--force` and asks nothing, because nothing it does
loses work. Stopping an already-stopped session succeeds and changes nothing.
A name no session holds is refused, the same way `attach` and `rm` refuse it —
`tmux` having nothing to end is what a typo looks like too.

`rm` reclaims the **worktree** — the directory — and keeps the branch.

### What `rm` destroys, and what it does not

**Everything `rm` takes away comes back on the next `start`, except
uncommitted changes.** The repo on the box is bare, so:

| | survives `rm` | comes back on `start` |
| --- | --- | --- |
| the branch and its commits | yes, in `repo.git` | yes |
| files smith placed into the worktree | no | yes, re-converged |
| uncommitted work — modified, staged, untracked | **no** | **no** |

So `rm` gates on exactly the two things that are not reversible or not idle: a
worktree holding uncommitted work, and a session with a process running in it.
On nothing else.

```sh
smith session rm dev smith-spec-42
```

```
session `smith/smith-spec-42` is running — `smith session stop smith-spec-42` first
worktree of session "smith-spec-42" holds uncommitted work: 2 modified, 1 staged,
0 untracked, and 3 commits not on any remote; reclaim it and lose that work with
`smith session rm smith-spec-42 --force`
```

The two refusals are printed **separately**: killing a process and destroying a
file are different losses, and collapsing them would make the output vague
exactly where it has to be precise. `--force` overrides both.

`rm` **never prompts**, with a terminal or without. A refusal prints what would
be lost and the exact command that proceeds, and exits non-zero — a delete has
no undo, and a `y/N` at a delete prompt is answered reflexively.

Two things that are reported and never refused:

- **Unpushed commits.** They are on the branch, and the branch survives:
  `session smith-spec-42 removed; branch spec-42 kept, 3 commits not on any remote`.
  A gate that fired on nearly every in-progress branch would train `--force`
  into muscle memory.
- **Files smith placed.** A placement's destination is subtracted before the
  worktree is judged dirty, so an `.env` smith wrote does not stand between you
  and a clean removal. The subtraction comes from the blueprint, not from any
  ignore file — emptying `repo.git/info/exclude` by hand does not change the
  verdict. A file that no placement declares is untracked work like any other,
  and it refuses.

## The readout before you destroy a box by hand

smith never destroys a box. You do that yourself, at your provider's dashboard,
and that click is irreversible and immediate: everything not pushed dies with
it. `list` is the one read that answers "is anything on this box unsaved?"
faster and more trustworthily than SSHing in and running `git status` in every
directory you remember creating.

```sh
smith session list dev
```

```
NAME             STATE    DIRTY  UNPUSHED
smith-main       live     -      0
smith-spec-42    stopped  dirty  3
web-hotfix       stopped  -      0

3 sessions · 1 dirty · 1 with unpushed commits
```

Four columns, and nothing else. `STATE` is `live` or `stopped`, probed on every
call — never remembered, so a `tmux` server killed outside smith reads as
`stopped` immediately. `DIRTY` is `dirty` or `-`. `UNPUSHED` counts the commits
no remote-tracking ref reaches, or `-` for a detached worktree, which has no
branch to count on.

**The summary line is always printed** — a reassuring read only reassures if it
cannot be absent:

```
3 sessions · all clean
```

```
no sessions

Start one with:
  smith session start --repo <name> --branch <name>
```

`list` **exits zero whatever it finds.** Something being unpushed is a property
of the data, not a command failing.

The read that clears a box for destruction is: every row `stopped`, every
`DIRTY` a dash, every `UNPUSHED` a zero — or, in one line, `N sessions · all
clean`.

Rows are flat, sorted by repo then branch, never grouped, never paginated and
never truncated: a group header breaks `grep`, `awk`, and pasting a name into
the next command. Scale is answered by the filters instead:

```sh
smith session list dev --repo smith      # one repo
smith session list dev --live            # only what is running
smith session list dev --stopped         # only what is not
```

`--live` and `--stopped` are opposites; asking for both is refused rather than
answered with an empty table, which reads exactly like a box with no sessions.

## Sweeping up

`--names` drops the table, the header and the summary and prints one bare name
per line — the one thing a batch removal is composed from:

```sh
smith session rm dev $(smith session list dev --stopped --names)
```

```
session smith-spec-42 removed; branch spec-42 kept, 3 commits not on any remote
session web-hotfix removed; branch hotfix kept
```

The batch is **all-or-nothing**. Every name is resolved and gated before any
worktree is touched, one offender refuses the lot, and every offender is named
in the same report:

```
worktree of session "smith-spec-42" holds uncommitted work: 2 modified, 0 staged,
0 untracked; reclaim it and lose that work with `smith session rm smith-spec-42 --force`
worktree of session "web-hotfix" holds uncommitted work: 1 modified, 0 staged,
0 untracked; reclaim it and lose that work with `smith session rm web-hotfix --force`
nothing was removed: `smith session rm` is all-or-nothing across the 2 sessions it was handed
```

A name no session holds refuses the batch too, before anything is removed.
Partial deletion is the state nobody can reason about afterwards.

Note that filters live on `list` and **not** on `rm`: `smith session rm dev
--stopped` is rejected as the unknown flag it is. The target set of a delete is
never implicit — you compose it, and you can read it first by running the
`list` half on its own. There is no reaper, no GC and no age policy; this
composition plus a human reading the inventory in between is the deliberate
answer.

## What is not here in v1

- **`--driver agent`**, or anything that launches an agent. Sessions are
  human-driven; the delegation loop is the AFK map's.
- **Keystrokes.** Everything smith does to a session it does by starting or
  stopping it. There is no `send-keys`.
- **Outcomes.** Liveness only — smith reports that a session is running, never
  whether what ran in it succeeded.
- **A browser terminal**, a per-session log, `--json` on `list`, and a box
  `destroy` verb. Deferred, and nothing here forecloses them.

Background reading:
[ADR-0005](./adr/0005-terminal-rides-the-ssh-door.md) for the terminal riding
the SSH door, [ADR-0008](./adr/0008-smith-runs-on-the-box.md) for why the verbs
run on the box, and
[ADR-0010](./adr/0010-session-teardown-safety-and-the-work-state-readout.md)
for the teardown gate and the work-state readout.
