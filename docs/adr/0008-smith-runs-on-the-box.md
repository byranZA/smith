# smith runs on the box; local smith is a control surface, not a second implementation

## Context

Until this decision, smith was entirely a laptop-side tool: it reached a box over SSH, ran a
bootstrap script there, and read a marker back. Sessions — a `tmux` session per worktree, driven
by a human or a coding agent — could plausibly have been driven the same way, with the operator's
smith orchestrating `tmux` remotely over `internal/connection`.

The test that settles it is the AFK case, which the downstream delegation-loop map exists to
build: under a laptop-driven model, a "truly AFK" run dies when the lid closes. AFK *requires*
something resident on the box. There is no version where the loop lives on the laptop and the
acronym is honest.

Deciding *where* smith runs then forces three follow-on questions this ADR also answers: how the
binary gets onto the box, what happens when the two sides drift, and what configuration the
on-box smith reads.

## Decision

**smith installs itself onto the box, and the session and workspace verbs execute there.** The
operator's local smith bootstraps the box and thereafter **relays** commands over SSH
(`ssh smith@box /usr/local/bin/smith session list`) rather than reimplementing them. There is one
implementation of every verb, and it is the identical surface an operator gets by SSHing in and
typing `smith`.

The session package therefore runs natively, via `os/exec`, and never over SSH. The SSH boundary
sits **above** it. (An earlier draft proposed abstracting `internal/connection` so session logic
could run either way; that idea is withdrawn — it abstracts a seam that does not exist.)

### The box fetches its own release asset, at local smith's version

Uploading the operator's binary is dead on arrival: the operator is likely `darwin/arm64` and the
box `linux/amd64`, and smith is a binary, not a toolchain, so it cannot cross-compile itself.
Embedding Linux binaries in every release would inflate the download for every user to serve the
VPS case.

So the box fetches from GitHub Releases — the asset contract
[#33](https://github.com/byranZA/smith/issues/33) locked
(`smith_<version>_linux_<arch>.tar.gz` + `checksums.txt`), arch from `uname -m`, verified against
the published checksums. A box that can `apt` can reach GitHub.

**The version fetched is local smith's own**, so the two sides match by construction rather than
by policy. When local smith is **not** a release build — `dev`, or a VCS pseudo-version — there is
no asset to fetch and smith **refuses**, with `--smith-version <tag>` as the explicit escape.
Silently resolving "latest" from a dev laptop installs a version the operator never chose.

### Install is its own stage, not a ninth bootstrap phase

A ninth entry in `bootstrap.sh`'s `PHASES` array would come with free marker recording and free
check-before-change, but it welds smith's own distribution into the script that provisions *any*
box, and this map's standing preference is that the base layer stays untouched.

Install is a **stage** in `machine setup`'s pipeline, driven from local smith over SSH — the
precedent being the tailscale access layer, already an admin-side stage that is not a bootstrap
phase. It is sequenced **after** bootstrap (it needs the `smith` user and the hardened door) and
**before** the workspace stage. That ordering is forced: on-box smith *runs* the workspace stage,
so whatever installs smith cannot itself be an on-box smith verb.

Two stages, in order, both operator-side writes:

1. **install** — converge `/usr/local/bin/smith` to local smith's version.
2. **stage config** — write the blueprint document and the resolved placement bytes into
   `/etc/smith/` (see below).

They are deliberately separate: the binary is smith's own distribution, versioned by a release
tag; the blueprint is the box's configuration, versioned by the operator. Folding them together
would make `machine upgrade` silently re-converge config.

### `/usr/local/bin/smith`, absolute-path relay

Root-owned, `0755`, on the default `PATH`. The relay invokes the **absolute path**, because a
non-interactive `ssh box smith …` gets a stripped `PATH` and "works when I SSH in" is exactly the
bug you do not want in the relay.

Being on `PATH` means an operator who SSHes in gets the real thing — a deliberate property, not a
side effect. Two benign edges: on-box smith has no cache
([ADR-0006](./0006-the-blueprint-config-surface.md) — a box knows of no other boxes), so `machine
list`/`add`/`forget` have nothing to work on, and a box has no reason to provision another box.

### Version skew is refused, not warned

**Terminology, because the word was already taken.** *Schema skew* (`marker.Skew`, exists today)
is the marker's `schema_version` against the constant this build understands — the shape of a file
smith reads. **Version skew** is the smith *binary* on the box against the smith *binary* on the
operator's machine — whether the command line local smith constructs is one the on-box binary can
parse and mean the same thing by. They are independent: a box provisioned by 0.1.0 and never
re-set-up can hold a schema-1 marker while running a 0.3.0 binary.

The relay invokes `/usr/local/bin/smith --relayed-from <local-version> <args…>`; on-box smith
compares against its own version and **exits non-zero on any mismatch**. One round trip, no
pre-flight probe, no staleness window between probe and call, and the enforcement sits on the side
that actually knows what it can parse.

`--relayed-from` is a **hidden, optional persistent flag** on the root command. Present → compare
and refuse. Absent → no check at all, because that is the human who SSHed in and ran a verb by
hand; they are not relaying and have nothing to compare against.

### Install and upgrade are one code path

Probe the installed version → no-op if it matches → otherwise download to a temp path → verify →
`install -m 0755` over the old one. Check-before-change like everything else; replacing the file
does not disturb an already-running process on Linux.

- `machine setup` converges the binary as one step of the larger pipeline.
- **`smith machine upgrade <name>`** runs that stage and nothing else — for the operator who
  upgraded their local smith and wants to move one box without re-converging its placements.

`machine upgrade` converges the box to **local smith's** version, so the post-condition is always
*no version skew* rather than *box is newest*. It will move a box backwards when local is older;
the name stays `upgrade` because that is what an operator reaches for, but the output states what
is happening: `box dev: 0.3.0 → 0.1.0 (matching local smith)`. It is **local-only**, because a box
cannot replace its own binary at the behest of a command running from that binary.

### On refusal: prompt when a human is there, message when not

The refusal prompts interactively **only when both stdin and stdout are a TTY** — stdout alone is
not enough, since a `< /dev/null` invocation must never hang. Default is **no** on a bare Enter.
On yes: converge the box, then **proceed with the original command**, otherwise the prompt is a
slower way to print a suggestion. On no, or with no TTY: a message naming the exact `smith machine
upgrade <name>` command, and a non-zero exit — the path an AFK loop inherits, and it never blocks.

The prompt fires in **both directions**, naming the direction plainly (`box dev runs 0.3.0, you
run 0.1.0 — converge box to 0.1.0? [y/N]`); suppressing it when local is older would leave the
operator with a refusal and no offered fix.

### What on-box smith reads: the blueprint document itself, staged verbatim

On-box smith is the *same binary* by construction, so it already contains the parser. Decomposing
the blueprint into derived artifacts — a `mise.toml`, a worktree layout, a placement manifest —
buys nothing and costs a second on-box format with its own schema, its own validation, and its own
skew axis. So the document is staged **verbatim and unfiltered** at `/etc/smith/blueprint.yaml`,
with the resolved placement bytes beside it at `/etc/smith/placements/`. Details, including why the
box gets no `~/.smith/` and why strict validation runs a second time, are in
[ADR-0006](./0006-the-blueprint-config-surface.md).

### v1 is HITL; the AFK loop is a downstream map

This map ships a box where repos, envs, worktrees and toolchain are stood up and the operator
works in the terminal. `--driver agent` does **not** ship in v1: with no agent adapter there is
nothing to launch, and shipping a flag that cannot act is worse than deferring it. The driver
remains a *recorded property* of a session — `list` shows it, and it is what tells you not to type
into a pane.

The guarantees this decision owes the downstream map, so it does not have to rediscover them:

- **The session primitive is dumb about work.** It takes `(repo, branch, base)` and has no concept
  of a spec, a task, or a dependency graph. That is what keeps it equally usable by a human.
- **The driver process is the tmux session's root process**, so its exit ends the session and
  **session-end *is* the completion signal** — no sentinel, no exit code, no watcher.
- **smith never sends keystrokes.** No `tmux send-keys`. Typing at a prompt and inferring success
  from screen text is unobservable, unverifiable, and racy. Everything smith does to a session, it
  does by starting or stopping it.
- **Liveness only, never outcome.** smith reports `running|stopped` from the live probe and never
  captures the driver's exit status.
- **smith's git surface is `worktree add` / `worktree remove`.** Commit, push, merge and PR are
  agent work driven by prompts.
- **Box credentials are driver-agnostic**, so AFK introduces no new credential concept — it
  consumes what HITL already required.
- The branch/worktree unit is an **execution** (1..N sequential tasks on one branch), with names
  derived loop-side.

## Considered options

- **Laptop-driven sessions over SSH** — rejected. It cannot support AFK at all, and it forces
  either two implementations of every verb (which drift) or an SSH/local abstraction inside the
  session package (a seam with one real user).
- **Upload the local binary** — rejected on cross-arch grounds: `darwin/arm64` operator,
  `linux/amd64` box, and smith cannot cross-compile itself.
- **A ninth bootstrap phase for install** — rejected. Free marker recording is not worth welding
  smith's distribution into the script that provisions any box.
- **Record the installed version in the marker** — rejected. `smith version` on the box is ground
  truth and cannot drift; a marker field can, and a half-failed upgrade leaves it lying. The
  marker's `smith_version` keeps its existing meaning — *the smith that provisioned the box*.
- **Warn on version skew and proceed** — rejected outright. A renamed flag under a warning
  produces wrong behaviour with a green exit. Exact-match is defensible precisely because `v0.x`
  promises no compatibility.
- **Auto-update** — ruled out of scope. It drags in when-to-check, whether a relay stalls on a
  network call, and an AFK loop mutating its own binary mid-run. The explicit surfaces (`machine
  setup`, `machine upgrade`, the TTY-gated prompt) ship instead.
- **Local self-upgrade (`smith upgrade`)** — ruled out of scope as a *distribution* concern: it is
  the hosted `install.sh` question that [the release map](https://github.com/byranZA/smith/issues/33)
  already ruled out of its own scope. An operator who `curl`s a new local smith and then converges
  their boxes is fully served.

## Consequences

- **Failure before the final `install` leaves the old binary untouched** and exits non-zero.
  Download and verify happen at a temp path; a checksum mismatch is a hard abort with no retry and
  no install-anyway fallback. A box whose upgrade fails is still a working box at its old version
  — the state you want when the failure is GitHub being down mid-run.
- **Exit 127 means "no smith on this box."** Every box provisioned by `v0.1.0` is in that state
  today. `127` or `No such file or directory` on the relay's first invocation must be classified
  and pointed at `machine setup` — not `machine upgrade`, since such a box predates the stage and
  likely wants the rest of the pipeline too. `machine upgrade` on it still works, so the message
  is a nudge, not a gate.
- **`machine upgrade` is a fourth local-only verb**, on different grounds from
  `machine list`/`add`/`forget`: those are local-only because a box knows of no other boxes, this
  one because of self-replacement. Stated separately so "local-only" is not read as implying
  "cache-related".
- **The TTY check is local smith's own `isatty`, before any SSH happens.** It is unrelated to
  `connection.defaultSSHOptions`' `BatchMode=yes` (which forbids *ssh* prompting for credentials)
  and unrelated to [ADR-0005](./0005-terminal-rides-the-ssh-door.md)'s `ssh -t` (which requests a
  TTY on the box for `tmux attach`). They compose: on the attach path the skew check and prompt
  run *before* local smith `exec`s `ssh -t` and hands over the terminal, and because
  converge-then-proceed runs over `BatchMode=yes` connections, answering yes can never surface a
  surprise password prompt underneath it.
- **Anything that adds a second implementation of a verb re-opens this decision.** A local
  fast-path for `session list`, a laptop-side worktree walk, a "just this once" local
  reimplementation — each recreates the drift this ADR exists to prevent.
- **A machine acting as its own box** (`session start` against localhost) is out of scope but is
  now a *degenerate* case rather than a second code path, because the binary is the same and the
  blueprint is staged verbatim. Such a machine would hold **both** `~/.smith/` (operator config
  home) and `/etc/smith/` (provisioned box state) — which is why those two must never share a
  directory.

See [issue #63](https://github.com/byranZA/smith/issues/63),
[issue #66](https://github.com/byranZA/smith/issues/66) and
[issue #75](https://github.com/byranZA/smith/issues/75).
