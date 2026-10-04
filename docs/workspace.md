# The workspace stage

A box that has been bootstrapped and handed its blueprint still has nothing to
work in. The **workspace stage** closes that gap: it converges the box to the
workspace its blueprint declares — credentials in place, packages installed,
runtimes pinned, and every declared repo cloned.

```
smith workspace converge [<box>]
```

It is the last stage of `smith machine setup`, which relays it to the box it
has just provisioned, and it is a verb of its own so you can re-converge a
workspace without re-running the whole pipeline.

The verb lives **on the box**, like the session verbs: naming a box relays it
over SSH to the smith installed there, naming none runs it here. So
`smith workspace converge dev` from your laptop and `smith workspace converge`
after SSHing in are one implementation of one verb. A box is resolved the way
it is everywhere else — a value containing `@` is an SSH target used as
written, a value without one is looked up in the
[box inventory](./inventory.md).

It runs on the box because that is where the work is
([ADR-0008](./adr/0008-smith-runs-on-the-box.md)): it installs packages and
clones repos onto the machine it is running on.

## What it converges, and in what order

The order is not arbitrary — each step puts something on the box the next one
needs:

1. **Placements** — box-scoped placements are materialized from their staged
   bytes. First, because this is what puts forge credentials on the box; a
   clone that runs before them fails with an authentication error that looks
   like a credential bug rather than the ordering bug it is.
2. **Identity** — the resolved git identity is set in the smith user's global
   git config. See [Git identity](#git-identity).
3. **Packages** — the blueprint's `packages` are installed with `apt`.
4. **Toolchain** — `mise` is installed, and smith writes the generated config
   pinning the blueprint's `tools` and exporting its `env`.
5. **Repos** — each declared repo is cloned bare into
   `<workspace>/<repo>/repo.git`, under `mise exec` so the blueprint's `env` is
   in scope for a token-authenticated clone.
6. **Orphans** — the workspace root is scanned for repos the blueprint no
   longer declares. This runs on every converge, even when the blueprint
   declares no repos at all.

No worktree is created and no repo-scoped placement is materialized: those
happen on `smith session start`. A converged box is one you can immediately
start a session on, not one that already has a checkout.

## Placements

Each box-scoped placement is written from the bytes `machine setup` staged, and
its `mode` decides what happens to a file the box already has:

- **`once`** keeps an existing file untouched (`kept <path>, which this box
  owns`), so a credential you rotated on the box by hand survives.
- **`converge`** rewrites a file whose content differs from the staged bytes.

Whatever the mode, a file whose content already matches has its permissions and
ownership brought back to what the placement declares (`corrected the
permissions of <path>`), without moving its modification time.

## Git identity

The `git` identity `smith blueprint check` resolved — `user_name` and
`user_email`, from the blueprint or your preferences — is set with one
`git config --global` per declared field, so commits made on the box are
authored as you declared. smith sets those two keys and does not own
`~/.gitconfig`: anything else in it survives.

- A value that already matches is reported unchanged (`user.name, user.email
  already set`), and a changed one replaces the old value.
- A field you did not declare is neither set nor unset. Dropping the identity
  from your config leaves the last one on the box, and a box with no declared
  identity runs no identity step at all.
- A write git refuses fails the identity step, and the stage exits non-zero.

Because a box placement to `~/.gitconfig` would replace the whole file and wipe
these keys, `blueprint check` refuses a git identity beside one.

## Packages

The blueprint's `packages` are the operator's list, installed by this stage —
they are not part of the base layer, whose own package set is smith's. They are
unversioned by design: `apt` covers the OS substrate and build prerequisites,
and version-pinned runtimes are the toolchain's job.

```yaml
packages:
  - ripgrep
  - jq
```

Only what the box is missing reaches `apt`, so a re-run of a converged box
reports that there was nothing to install and runs nothing. The install carries
the base layer's own first-boot lock wait, so a converge racing the
`unattended-upgrades` run of a freshly booted cloud image waits it out rather
than aborting.

## The toolchain

`mise` gives the box its version-pinned runtimes. smith installs it as a single
binary — not an apt package, but from `https://mise.run` into `~/.local/bin/mise`
— on a box that does not already have one on `PATH`.

smith declares versions by writing a config file it **fully owns** and then
running `mise install`. It never runs `mise use`, which would write into
`~/.config/mise/config.toml` — that file is mise's own, it may hold your
settings, and smith writes generated files by whole-file replacement. smith
neither reads nor writes it.

The blueprint's box-level `tools` and `env` land in
`~/.config/mise/conf.d/smith.toml`:

```yaml
tools:
  node: "20"
  go: "1.23"
env:
  GITHUB_TOKEN: env:GITHUB_TOKEN
  NODE_ENV: literal:production
```

```toml
# Generated by smith — do not edit.
#
# ...

[tools]
go = "1.23"
node = "20"

[env]
GITHUB_TOKEN = "ghp_…"
NODE_ENV = "production"
```

Each `env` value is a reference, and it is resolved **on your machine**, not
the box: `machine setup --blueprint` reads `env:GITHUB_TOKEN` from your shell
and `file:` paths from your disk, and stages the values to
`/etc/smith/env.json` on the box. A reference that does not resolve refuses
setup before anything reaches the box (`N reference(s) will not resolve, so
nothing was staged:`, then one line per variable). The toolchain step only
reads the staged values; a variable with none fails the step by name
(`export <VAR>: …`) rather than falling back to whatever the box holds.

So **`smith workspace converge` alone does not pick up a changed value**: after
rotating a token, re-run `smith machine setup <box> --blueprint <name>` to
re-stage it. The fragment holds resolved values, so it is written `0600` and
owned by the smith user.

The file is written only when its bytes differ, so a converged re-run moves no
modification time — and a version the blueprint has changed rewrites it and
installs the new one.

Per-repo `tools` and `env` land in `<workspace>/<repo>/mise.toml` — the same
mechanism, one directory above every worktree, so mise's walk-up finds it and
smith never dirties a checkout. It is also why a generated file holding
resolved secret values cannot be committed by accident. A repo that overrides
nothing takes the box's toolchain and gets no file of its own.

There is **no per-repo mise environment**: the config only *selects* a version
out of the one shared install store, so two repos declaring `node "22"` share a
single install.

**`conf.d` sits below `~/.config/mise/config.toml` in precedence**, so a
hand-written global config on the box outranks the blueprint. This is the same
precedent as a `mise.toml` committed inside a repo beating the one smith
generates: the nearer, more specific declaration wins, and you can override a
version without fighting a generated file.

Invocation is **`mise exec`, never `mise activate`** — `activate` does not fire
in the non-interactive and daemon-started shells smith and its agents run in.
Shims on `PATH` are the courtesy for a human who SSHes in.

## Repos

A repo the box does not have yet is cloned bare into
`<workspace>/<repo>/repo.git`. The workspace root is the one `machine setup` resolved on your machine and staged in `/etc/smith/resolved.json`, wherever along the chain you set it — flag, blueprint or preferences — and `~/workspace` when you set it nowhere. Each
repo directory is `0700`. A repo the box already has is fetched
(`git fetch origin`), never deleted and cloned again, because it may carry
worktrees with work that is pushed nowhere.

A clone whose `origin` differs from the blueprint's `url` is not repointed. That
repo fails with `the clone at <path> is of <url>, not the <url> the blueprint
declares: move or remove that directory to clone the declared url`, and the
other repos still converge.

## Orphans

A repo you drop from the blueprint stays on the box. The orphans step reports it
(`orphans left in place under <root>: a, b`, or `no orphans under <root>`) and
never deletes it. Only a directory holding a `repo.git` counts, so a directory
you made under the workspace root yourself is never reported.

## What it reports, and what it refuses

Each step reports as it finishes, with the commands' own output streaming live,
followed by a summary:

```
  packages    installed ripgrep, jq
  orphans     no orphans under /home/smith/workspace
workspace: 2 steps converged
```

The count is one per unit: each placement, each repo and each toolchain file is
a unit of its own. A run with failures ends `workspace: N steps converged, M
failed` and restates each failed line, so it is the last thing on screen.

The stage **never deletes**. A step it cannot converge is reported and the run
exits non-zero, having left what the box already holds exactly where it was.

Its two refusals are the staged-config contract's, inherited rather than
redefined: a box with no staged blueprint exits `4` and a staged document that
cannot be trusted exits `5`, both naming `smith machine setup` — the only
writer of either. The staged resolution is refused the same way: a box staged by a smith that predates it exits `4`, and one that is not valid JSON exits `5`. Neither falls back to a default; re-run `machine setup` from your machine.
