# smith on the box

smith does not drive a box from your laptop. `machine setup` **installs smith
onto the box**, and from then on the verbs that do work there — the session
verbs and `workspace converge` — execute on the box. Your local smith is a
control surface: it **relays** the verb over SSH and streams the box's own
output back.

```
smith session list dev          # from your laptop, relayed over SSH
smith session list              # after SSHing in — the same implementation
```

There is one implementation of every relayed verb, and it lives on the box
([ADR-0008](./adr/0008-smith-runs-on-the-box.md)). The reason is the AFK case:
a loop driven from a laptop dies when the lid closes.

Because smith is installed at `/usr/local/bin/smith`, an operator who SSHes in
and types `smith` gets the real thing rather than a stripped-down remote helper.
That is deliberate, and the relay depends on it too: it invokes that absolute
path, because a non-interactive `ssh box smith …` gets a stripped `PATH` and
"works when I SSH in" is the one bug the relay must not have.

## Two benign edges

Neither of these is a bug, and both look like one at first:

- **on-box smith has no box inventory.** The inventory (`~/.smith/cache/boxes.json`)
  is your laptop's name → address book. On a box it is empty, so
  `machine list`, `machine add` and `machine forget` have nothing to work on.
- **a box does not provision other boxes.** `machine setup` and `machine upgrade`
  are driven from your machine. `upgrade` is local for a second, unrelated
  reason: a box cannot replace the binary that the running command *is*.

## The install stage

`machine setup` runs an ordered pipeline of named **stages** after every
bootstrap phase has completed. Each reports itself by name:

| stage | what it does |
| --- | --- |
| `access` | enrolls the box on the tailnet and closes public SSH (tailscale mode only) |
| `install` | converges the box's smith binary |
| `config` | stages the blueprint and its placement bytes onto the box |
| `workspace` | converges repos, packages and toolchain — relayed to on-box smith |

A stage is not a bootstrap *phase*: phases are the base layer, run by
`bootstrap.sh` on the box and recorded in the marker; stages run from your
machine, on top of a box that is already provisioned, and are recorded nowhere.
The order is forced — `install` must precede `config` and `workspace` because
on-box smith is what runs the workspace stage.

The install stage never uploads your binary. You are likely `darwin/arm64` and
the box `linux/amd64`, and smith is a binary rather than a toolchain, so it
cannot cross-compile itself. Instead the box fetches its own release asset:

1. **probe** — the box reports its machine hardware name (`uname -m`, mapped
   `x86_64` → `amd64`, `aarch64` → `arm64`) and the smith version it already
   runs. An architecture smith publishes no asset for is refused by name, with
   nothing downloaded.
2. **no-op** — a box already at the version downloads nothing and says so.
3. **download** — `smith_<version>_linux_<arch>.tar.gz` and `checksums.txt`
   from GitHub Releases, to a temp path.
4. **verify** — the archive is checked against the published checksums. A
   mismatch aborts, is not retried, and installs nothing.
5. **install** — the binary is replaced in one step, root-owned and `0755`.

Because the replacement is the last step, every failure before it leaves the
box's existing binary untouched: a box whose upgrade failed is still a working
box at its old version.

Finally the stage **proves** what it did — it asks the newly installed binary
its version *through the relay*, declaring the version that installed it. A box
that came out of this stage is a box the relay is known to work against. (The
initial probe deliberately declares no version; a probe that declared one would
be refused by the very skew it exists to detect.)

## `machine upgrade`

```sh
smith machine upgrade dev
```

`upgrade` runs the install stage and nothing else: no bootstrap phase runs, no
configuration is staged, no placement is touched, and the marker is not
written. It exists for the operator who upgraded their laptop's smith and wants
to move one box without re-converging everything on it. It also works on a box
that has never had smith — it installs it.

**It converges the box to *your* version, so it will move a box backwards.**
The post-condition is *no version skew*, not *the box is newest*, and the
report says which way it went:

```
box dev: smith 0.3.0 → 0.2.0 (matching local smith)
box dev: installed smith 0.2.0
box dev: smith 0.2.0 already matches local smith
```

Both verbs take `--smith-version <tag>` to install a version you name instead
of the one you run — the escape a build with no release needs, and the way to
pin a box to an older smith.

## Version skew

**Version skew** is the smith *binary* on the box against the smith *binary* on
your machine. Local smith constructs a command line and on-box smith parses it;
between versions a renamed flag does not fail, it produces wrong behaviour under
a green exit. So the relay declares the version it is speaking, and the box —
the side that actually has to parse the command line — **refuses** any mismatch,
in either direction. It is refused, never warned.

A human is not checked. Someone who SSHed in and typed `smith session list` is
not relaying and has nothing to compare against, so the declaration is absent
and no comparison happens.

When a relay is refused, local smith reacts based on whether a human is there —
both stdin and stdout must be a terminal, and the check happens on your machine
before anything travels:

```
box dev runs 0.3.0, you run 0.2.0 — converge box dev to 0.2.0? [y/N]
```

Answering `y` converges the box and then **runs the original command**;
anything else — a typo, a bare Enter — declines, because the question mutates a
box. Declining, or having no terminal, prints the exact command instead and
exits non-zero:

```
converge the box first:
  smith machine upgrade dev
```

That is the path an AFK loop inherits, and it never blocks.

### A box with no smith on it

Every box provisioned before smith installed itself is in this state. A relay to
one is classified rather than surfaced as a generic remote-command failure, and
points at `machine setup` — not `machine upgrade`, since such a box likely wants
the rest of the pipeline too:

```
box dev has no smith installed: `smith machine setup dev` installs it, along
with the rest of what the box is missing
```

### Version skew is not schema skew

Two mismatches share the word *skew* and they are different axes:

| | version skew | schema skew |
| --- | --- | --- |
| what is compared | the smith **binary** on the box vs. on your machine | the marker's `schema_version` vs. what the running build understands |
| detected by | the relay's declared version, refused by on-box smith | reading `/etc/smith/bootstrap.json` |
| fixed by | `smith machine upgrade <box>` | a smith that understands that schema |

They move independently: two smiths can agree on the marker's schema and still
be unable to parse each other's command lines.

## The marker records nothing about the installed binary

`/etc/smith/bootstrap.json` keeps the meaning it always had — its `smith_version`
is *the smith that provisioned the box*, not the smith installed on it.
`smith version` on the box is ground truth for what is installed, and it cannot
drift; a stored copy could, and a half-failed upgrade would leave it lying. So
neither `machine setup`'s install stage nor `machine upgrade` writes the marker.

## Contributing: a dev build cannot install

A build from a checkout — `make build`, or `go build ./cmd/smith` — reports its
version as `dev`. There is no `dev` release, so there is no asset for a box to
fetch, and smith **refuses to guess**: silently resolving "latest" would install
a version you never chose.

The practical consequence, on day one: **a from-source smith cannot complete
`machine setup`.** It bootstraps the box all the way through every phase and
then fails at the `install` stage alone, so a dev build is still useful for the
whole base layer. `machine upgrade` refuses before it opens a connection.

`--smith-version <tag>` is the escape:

```sh
smith machine setup root@203.0.113.10 --smith-version 0.2.0
smith machine upgrade dev --smith-version 0.2.0
```

Read the second half of the refusal, though: the *released* smith that flag
installs will then **refuse to relay a command from your dev build**, because
the box enforces that both sides match and a dev build has no released version
to match. So `--smith-version` gets a box provisioned; it does not get you a
working relay from a dev build. The fix on both sides is a released local smith
— install one from the [releases page](https://github.com/byranZA/smith/releases)
and drive the box from it.

A version-stamped build without a release download works too:
`go install github.com/byranZA/smith/cmd/smith@v0.2.0` reports `0.2.0` and
installs and relays like any release build.
