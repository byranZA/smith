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
   bytes. First, because this is what puts a git identity and forge credentials
   on the box; a clone that runs before them fails with an authentication error
   that looks like a credential bug rather than the ordering bug it is.
2. **Packages** — the blueprint's `packages` are installed with `apt`.
3. **Toolchain** — `mise` is installed, and smith writes the generated config
   pinning the blueprint's `tools` and exporting its `env`.
4. **Repos** — each declared repo is cloned bare into
   `<workspace>/<repo>/repo.git`, under `mise exec` so the blueprint's `env` is
   in scope for a token-authenticated clone.

**Only the packages step is built today.** The rest land in the slices that
follow, in the order above.

No worktree is created and no repo-scoped placement is materialized: those
happen on `smith session start`. A converged box is one you can immediately
start a session on, not one that already has a checkout.

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

## What it reports, and what it refuses

Each step reports as it finishes, with the commands' own output streaming live,
followed by a summary:

```
  packages    installed ripgrep, jq
workspace: 1 step converged
```

The stage **never deletes**. A step it cannot converge is reported and the run
exits non-zero, having left what the box already holds exactly where it was.

Its two refusals are the staged-config contract's, inherited rather than
redefined: a box with no staged blueprint exits `4` and a staged document that
cannot be trusted exits `5`, both naming `smith machine setup` — the only
writer of either.
