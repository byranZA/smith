# smith

The domain language of smith — turning a fresh VPS into a provisioned, secured, reachable
remote dev machine, then running coding agents on it. This file is the glossary only;
the *why* and the boundaries live in [`AGENTS.md`](./AGENTS.md), architectural decisions in
[`docs/adr/`](./docs/adr/).

## Language

### The machine and getting in

**Box**:
The Ubuntu LTS VPS smith turns into a dev machine — either one the operator brings and hands
over, or one smith creates through a provider adapter. Smith provisions it and, given an adapter,
creates and lists it — **v1 never destroys a box**
([ADR-0007](./docs/adr/0007-the-provider-adapter-is-data.md)) — but is not itself the provider.
It *is* the sandbox, not a container smith runs.
_Avoid_: server, host (except in `<login>@<host>`), instance, VM.

**Operator's machine**:
The machine the operator configures smith from and drives boxes from — where the config home
lives, blueprints are written and secret references are resolved. The other side of every split
between local and box: config home against box state, local smith against on-box smith. `smith
init` scaffolds it; nothing on a box does.
_Avoid_: admin machine, local machine, workstation, control surface (the local smith binary's role,
not the machine).

**Bootstrap-in path**:
The one-time public-SSH entry smith uses to reach a brand-new box — `smith machine setup
<login>@<host>` over system `ssh`/`scp`, authenticating with the key the provider already
injected. Distinct from the ongoing `ssh smith@host` reach the setup establishes.
_Avoid_: onboarding, first connection.

**Bootstrap login**:
The root-or-sudo-capable account the operator connects *as* during the bootstrap-in path.
Under a non-root login smith runs via `sudo` and fails early if passwordless sudo is absent.
Its reach ends with the phases: `ssh-hardening` may close it (a root login always is), so
nothing after the phases travels as it — every stage reaches the box as the smith user.
_Avoid_: admin user, initial user.

**smith user**:
The provisioned account smith creates on the box and the operator uses thereafter
(`ssh smith@host`). Gets passwordless sudo — a deliberate blast-radius tradeoff for a
self-managing dev box.

### What "done" means

**Provisioned / Secured / Reachable**:
The three-part destination state of a bootstrapped box. *Provisioned* = smith user, dirs, and
base packages present. *Secured* = hardened SSH, `ufw` default-deny, `fail2ban`, unattended
security upgrades. *Reachable* = the operator can `ssh smith@host` (public or over the tailnet).

**Base layer**:
The always-applied slice of bootstrap — smith user + hardening — identical regardless of how
the box is reached.
_Avoid_: base hardening (as a distinct thing), core setup.

**Access layer**:
The one pluggable choice layered over the base: how the box is reached. *public* leaves hardened SSH open on the public IP; *tailscale* joins a tailnet and closes public SSH. The mode is a field of the **resolved configuration**, so setup acts on the same value `blueprint check` reports: flag, then blueprint, then preferences, then *public*. A box whose marker records *tailscale* is never taken back to *public* unless the operator's flag says so; reopening public SSH is a decision about one box, not a side effect of a blueprint or a default. Architected as a swappable layer so neither mode re-architects the other.
_Avoid_: connectivity mode, networking option.

**Lock-out safety**:
The invariant that smith never severs its own reach to a box before verifying the new door
opens — "never close the door blind." Applies to both access modes: in tailscale mode, probe
`ssh` over the tailnet *before* closing public SSH; any failure leaves the old door open and
reports.
_Avoid_: rollback, failsafe.

### State on the box

**Phase**:
One named, ordered, mutating step of the base-layer bootstrap sequence — `packages`,
`smith-user`, `smith-keys`, `firewall`, `ssh-hardening`, `fail2ban`, `auto-updates`, `access`
(preceded by a non-recorded `preflight` gate). The names *are* the marker's `completed_phases`
values, so they are load-bearing. A phase appends itself to the marker only on success; the
reachability-affecting phases (`firewall`, `ssh-hardening`, `access`) carry the lock-out gates,
and only `ssh-hardening` closes a base-layer door — which it self-reverts on any local failure,
so a mid-sequence failure is never a lock-out. Distinct from a *stage*.
_Avoid_: step, task.

**Stage**:
One step of `machine setup`'s pipeline on the operator's machine, driven from local smith over SSH rather than by
`bootstrap.sh` — `access` (the tailscale access layer), `install` (the smith binary), `config`
(staging the blueprint), `workspace`, in that order. The names are what each stage reports itself
as. Unlike a phase, a stage is not in the marker's `completed_phases` and does not touch the base
layer; a failed stage names itself, skips every stage after it, and exits partial, because the box
is provisioned and what stopped sits on top of it. `install` and `config` must precede `workspace`,
because on-box smith is what runs it ([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)), and
`install` is also reachable alone as `smith machine upgrade`. Every stage reaches the box as the
smith user, never the bootstrap login — over the public host until `access` proves the tailnet
door, and over the tailnet after.
_Avoid_: phase (reserved for the base-layer sequence), step.

**Shipped script**:
A script smith copies onto the box to drive one command — `bootstrap.sh` for setup's phases, the
tailscale access stage and status, `install.sh` for the install stage — and removes when that
command ends, or sooner when the stage or subcommand that shipped it finishes with it. smith drives
it by **subcommand**: each call runs one named subcommand of the script (`preflight`, `setup`,
`enroll`, `probe`, …), never the script as a whole. Unlike the staged
blueprint, the staged env or a placement, it is **not box state**: it belongs to the login that
shipped it, never outlives the command, and no two commands or logins share one. A cleanup that
fails never fails the command
([issue #217](https://github.com/byranZA/smith/issues/217)).
_Avoid_: staged script (staged is reserved for what persists in `/etc/smith/`), payload.

**Marker**:
The versioned on-box record of what bootstrap did — `/etc/smith/bootstrap.json` (schema + smith
version + access mode + completed phases + timestamp, plus the box's self-record: which blueprint
it was built from, the operator-chosen box name, and the provider destroy reference to tear it
down by hand). Records **nothing** about the installed smith binary or the create-time SSH key —
both are probed, not stored. Lets `smith machine status`
read state back, any operator's machine re-run idempotently, and smith recognise a box from just its IP. A **ledger, not a gate**: every `setup` run
executes all phases and check-before-change makes done ones no-ops — `completed_phases` records
progress (for failure reports and `status`), it never *skips* execution.
_Avoid_: state file, lockfile, manifest.

**Check-before-change**:
The rule that every bootstrap step verifies the box's current configured state before mutating
it, so re-runs converge instead of duplicating. Governs *configured state*, not the package
manager (which is simply `ensure`d, not probed).
_Avoid_: idempotency (as the term of art — check-before-change is *how* smith achieves it).

**Drift**:
Divergence between what the marker records and what smith re-probes live on the box.
`smith machine setup` mutates to converge; `smith machine status` only *reports* drift and never
remediates it — the read-only, state-oriented half of the setup/status seam.
_Avoid_: skew (reserved for the two version mismatches below), diff.

**Schema skew**:
The marker's `schema_version` against the constant the running build understands — the shape of a
file smith reads. `marker.Skew`.
_Avoid_: version skew (a different axis).

**Version skew**:
The smith *binary* on the box against the smith *binary* on the operator's machine — whether the
command line local smith constructs is one on-box smith can parse and mean the same thing by. The
relay passes a hidden `--relayed-from <version>` and on-box smith **exits non-zero on any
mismatch**; it is refused, never warned, and an absent declaration is the human who SSHed in, who
is not checked. Local smith reacts to the refusal: it offers to converge the box when both its own
streams are a terminal and runs the original command once it has, and otherwise prints
`smith machine upgrade <box>` and exits non-zero. A box answering with exit 127 is a box with no
smith installed, classified as such and pointed at `machine setup`. Converging is always to
*local's* version, so the post-condition is no skew rather than a newest box. Independent of schema
skew ([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)).
_Avoid_: drift, schema skew.

### Secrets

**Reference, not value**:
The rule that config points *at* a secret (a `scheme:arg` like `env:VAR` or `file:/path`),
never embeds the secret itself. The durable primitive the secret surface is built on.
_Avoid_: secret string, credential literal.

**Operational secret**:
A secret consumed during bootstrap and then discarded — never persisted on the box. The
Tailscale auth key is the only one in the setup domain; it travels over SSH stdin, never argv.
_Avoid_: transient secret, ephemeral credential.

**Provisioned secret**:
A secret *installed onto* the box for later use — a forge token, a coding-agent API key, a
credential file. Distinct lifecycle and surface from operational secrets: it arrives by the one
placement/`env` path, resolved on the operator's machine at `machine setup`, and **sits on the box in
plaintext**. At-rest protection is a stated non-goal; `perms` is an accidental-exposure guard, and
blast radius plus box disposability are the control
([ADR-0009](./docs/adr/0009-provisioned-secrets-sit-in-plaintext.md)).
_Avoid_: stored secret, deployed credential.

**Probe key**:
The throwaway keypair the `ssh-hardening` self-test mints to drive its loopback login — *born on
the box, used on the box, scrubbed on the box, never transmitted*. Neither an operational nor a
provisioned secret: it grants no durable access and mints nothing that outlives the probe. Its
public half is appended to `smith`'s real `authorized_keys` (so the live handshake exercises that
file's perms/ownership/StrictModes) and removed by marker afterward; its private half lives in a
`mktemp -d` and drives one `ssh smith@localhost`. It exists so the gate can prove *a* pubkey login
survives hardening without depending on the operator's key.
_Avoid_: test key, temp credential, self-signed key.

### Tailscale access

**tag:smith**:
The ownership tag every smith-enrolled node carries. Kept purely so the node's key never
expires; requires two one-time-per-tailnet user prerequisites (`tagOwners` and an `ssh` ACL
rule) that smith's auth key cannot self-serve.

**Tailscale SSH**:
Keyless SSH over the tailnet — Tailscale is the identity authority, replacing `authorized_keys`
for tailnet connections without touching `sshd`. The v1 tailscale-mode reach.
_Avoid_: tailnet SSH, VPN SSH.

### OS support

**Floor + capability gate**:
How smith decides a box is supported: an LTS *floor* (`ID=ubuntu`, an `LTS` release,
`VERSION_ID` ≥ 24.04, no upper bound) plus *capability probes* for what smith actually needs
(apt, systemd, packages) — never a hardcoded enumeration of releases. Future LTS releases pass
automatically; an unmet need fails loudly at the step, not pre-emptively at the gate.
_Avoid_: version allowlist, supported-OS list.

### Declaring and creating a box

**Blueprint**:
The reusable, declarative description of a dev box smith stands up — its repos, access mode,
environment, seed data, toolchain, and terminal. One blueprint instantiates many boxes; it is
the box's *desired state*, which setup converges to. The config surface smith gained when the
provisioning map arrived — distinct from the marker, which records what a *particular* box did.
One YAML file per blueprint at `~/.smith/blueprints/<name>.yaml`, strictly validated, and **staged
verbatim onto the box** at `/etc/smith/blueprint.yaml` so on-box smith reads the same document
([ADR-0006](./docs/adr/0006-the-blueprint-config-surface.md)).
_Avoid_: manifest (reserved, avoided for the marker), config file, template, profile.

**Config home vs box state**:
`~/.smith/` is the **operator's** config home — `blueprints/`, `preferences`, and a gitignored `cache/` holding the box inventory. `/etc/smith/` is a **provisioned box's** state — the marker, the staged blueprint, the staged placement bytes, the staged env, and the staged resolution. Separate locations and roles, same format; they must never share a directory, because one machine could one day hold both. The config home is created by the **first write** into it and never by a read, and its `.gitignore` is appended to rather than rewritten: the file is the operator's.
_Avoid_: config dir (ambiguous between the two).

**Staged blueprint**:
`/etc/smith/blueprint.yaml` — the operator's blueprint document written onto the box by the
config-staging stage, **verbatim**: the same YAML they wrote, byte for byte, no field filtered
and no derived format. `machine setup` is its sole writer, and on-box smith reads it rather than
fetching configuration of its own. Root-owned and `0644`, because it is a committed, non-secret
artifact an SSHed-in operator is meant to be able to read.
_Avoid_: box config, rendered blueprint.

**Staged env**:
`/etc/smith/env.json` — the value of every variable the blueprint's `env` declares, box-scoped and
per-repo, **resolved on the operator's machine** at `machine setup` and staged beside the document.
It exists because `env:GH_TOKEN` names a variable in the *operator's* shell and `file:` a path on
*their* disk: a box re-reading either would export something else entirely, or nothing
([ADR-0009](./docs/adr/0009-provisioned-secrets-sit-in-plaintext.md)). The staged blueprint stays
verbatim — the references are still in it as dead provenance — and on-box smith reads a value by
name rather than resolving one. A declared variable with no staged value is refused by name, never
filled in from the box. Smith-owned and `0600`: these are provisioned secrets, unlike the
world-readable document beside them.
_Avoid_: on-box resolution, box environment.

**Staged resolution**:
`/etc/smith/resolved.json` — the box's **resolved configuration**, every fixed-key field but `provider`, **resolved on the operator's machine** at `machine setup` and staged beside the document. It exists for the same reason the staged env does: a field set only in the operator's preferences is invisible to a box, which is deliberately given no config home. On-box smith reads these values rather than resolving anything itself, so the box acts on the values `blueprint check` showed the operator. A box with a staged blueprint but no staged resolution is refused with "re-run `machine setup`", never filled in from defaults. Root-owned and `0644`: nothing in it is secret.
_Avoid_: box preferences, on-box resolution.

**Preferences**:
`~/.smith/preferences.yaml` — the operator's own half of the config home, answering *if you built
a completely different box tomorrow, should this value follow?* with yes. **Fixed-key fields
only** (`access`, `terminal`, `workspace`, `provider`, `git`); the open-ended collections
(`repos`, `placements`, `packages`, `tools`, `env`) are blueprint-only, so the marker's blueprint
pointer is enough to reproduce a box. Optional, like the blueprint itself
([ADR-0006](./docs/adr/0006-the-blueprint-config-surface.md)).
_Avoid_: settings, defaults, user config, global config.

**Starter**:
A commented config file `smith init` writes into an empty config home — a starter preferences file and a starter blueprint — valid as written and resolving to built-in defaults until the operator uncomments something. Distinct from the **worked examples** in the docs, which are filled in end to end to show the whole surface; a starter is the blank to fill, an example is the filled one to read. Written only where no file exists, never over one. `smith repo init` writes the repo file's starter the same way.
_Avoid_: template (reserved for an adapter's command lines), skeleton, default config.

**Resolved configuration**:
What smith would actually use for one box, every field carrying the **origin** it came from —
flag, blueprint, preferences, or built-in default. Precedence is field-level and most-specific
wins (**CLI flag > blueprint field > home preference > built-in default**), with one exception:
the `provider` block **replaces wholesale**, because half of one adapter beside half of another
is never wanted and the mismatch would surface only at box creation. Origins are returned data,
not printed side effects, which is why `blueprint check` is a formatter over them.
_Avoid_: merged config, effective settings, final config.

**Credential-agnostic**:
The stance that smith **places files and exports environment variables and knows no service by
name** — not GitHub, not npm, not any forge. The moment it knew one it would owe the next, plus
CLI version skew and a choice between auth modes. The concrete cost is real and paid in
documentation, not magic: a blueprint declaring `GITHUB_TOKEN` beside `https://` clone URLs does
not authenticate, because git needs a credential helper and smith neither notices nor fixes that.
The one exception is the loop's **tracker**, which knows GitHub Issues by name through `gh`'s
own auth and holds no credential (ADR-0012).
_Avoid_: provider-agnostic (reserved for the provider adapter), integration.

**Box inventory**:
`~/.smith/cache/boxes.json` — a name → proven-target address book and nothing else. An entry is an operator-chosen name plus the opaque SSH target smith **proved** works. Identity only: access mode, phases, blueprint pointer and destroy reference all stay marker-side, so there is nothing in it that can silently go stale — hence no TTL and no `last_seen`. Written by a successful `machine setup` and by `machine add`, which is read-only towards the box; `machine rename` moves an entry and the name on the box's marker together; `machine list` is instant and offline (`--probe` reports reach without storing it); only `machine forget` removes. **Rebuildable, not self-rebuilding**: a deleted file loses nothing unique, but v1 has no discovery path, so it is rebuilt by hand, one `machine add` per box ([ADR-0011](./docs/adr/0011-the-box-inventory-is-identity-only.md), [docs/inventory.md](./docs/inventory.md)).
_Avoid_: registry, database, state store.

**The "@" decides**:
The one resolution rule every verb shares. A value containing `@` is a literal SSH target, used as
written and never looked up; a value without one is looked up in the **box inventory** and, on a
miss, handed to `ssh` verbatim — so an `ssh_config` alias or a MagicDNS name works with no smith
configuration, and an inventory name shadows an identically-named alias. No flag and no sigil, and
the target is never parsed (*reference, not value*, extended to reach)
([ADR-0011](./docs/adr/0011-the-box-inventory-is-identity-only.md)).
_Avoid_: alias resolution, host lookup, DNS.

**Provider adapter**:
The operator-supplied description of one provider's CLI — command templates plus field extractors,
all emitting JSON, normalized to `{id, ip|null, marker}` — through which smith creates and lists
boxes without embedding any provider SDK. **It is data, not code**: smith holds no provider
knowledge at all. The contract specifies `create`/`list`/`destroy`; v1 invokes only the first two.
Optional: absent an adapter, the operator creates the box and hands smith `<login>@<host>`. The
whole `provider` block **replaces wholesale** — the single exception to field-level precedence,
because half of one adapter merged with half of another is never wanted
([ADR-0007](./docs/adr/0007-the-provider-adapter-is-data.md)).
_Avoid_: provider plugin, driver (reserved for a session's actor), integration.

**Provider marker**:
How an adapter recognises a box it created — a tag, a label, or the box's own name, the adapter's
choice, since smith must not require native provider tags. Three fields — `arg`, `read`, `expect` —
because what `create` passes, where it reads back from and the form it reads back in all differ per
provider. Its value is the operator-chosen box name, so two smith boxes cannot share a name at one
provider; smith does not enforce that. v1 writes a marker and never reads one back, so `read` and
`expect` are validated and unused. Unrelated to the on-box **marker**.
_Avoid_: tag (the old name; it presumed native tag support).

**Canonical box record**:
What any provider's JSON is normalized to — `{id, ip|null}` — so no caller of the
provider adapter ever touches a provider's own shape. The marker is stamped on create and, since
v1 never reads one back, is not a field of the record. An address the provider has not assigned
yet is null, and that absence is what makes smith poll `list`, rather than a declared field
saying the provider is synchronous or deferred.
_Avoid_: droplet, server object, instance record.

**Extractor path**:
The small path grammar an adapter reads a provider's JSON with — a key, dotted descent, `[*]`,
and `[field=value]` selecting array elements by field value. A `record` path locates the box
inside whatever envelope a response wraps it in, per template; the `extract` paths then read id
and address relative to it. There is no positional index, deliberately: a provider's address
list has no stable order, so selecting by position silently yields a private address. A path
matching nothing is an error naming the path, never a zero value.
_Avoid_: JSONPath (the standard grammar, which this is not), selector, query.

**Placeholder**:
One of the four values smith substitutes into an adapter's templates — `{{name}}`,
`{{marker_arg}}`, `{{ssh_key}}`, `{{id}}` — every other argument being the operator's own
literal text. An unrecognised one is a validation error, and an argument that renders empty
drops itself *and* the flag before it, which is how a flat argument list says "omit this".
_Avoid_: variable, parameter, interpolation.

**Control surface / relay**:
The operator's local smith after bootstrap: it holds the box inventory and the config home, and it
**relays** the session and workspace verbs over SSH to the smith installed on the box rather than
reimplementing them. One implementation of every verb, identical to what an operator gets by SSHing
in and typing `smith` ([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)).
_Avoid_: client, remote execution, agent.

### Working on the box

**Session**:
A running `tmux` session in a worktree, with a driver (human or agent), that the operator
attaches to read-only or writable. The unit of work on a box; many run at once. "HITL" and
"AFK" are informal labels for its two common shapes, not domain types. Named `<repo>-<branch>`
(sanitized), which is the string every verb takes; the tmux session is `smith/<name>`. Verbs are
`start` / `attach` / `list` / `stop` / `rm`, they **execute on the box**, and running-state is a
live `tmux has-session` probe — no daemon.
_Avoid_: terminal, tab, pane.

**Worktree**:
A git worktree of a managed repo on the box — the unit of parallel work, one per session. Lets
several sessions (human or agent) work a repo's branches side by side without colliding.
_Avoid_: checkout, clone.

**Workspace**:
Where managed repos live on the box: `~/workspace/<repo>/repo.git` — a **bare** repo — with
`worktrees/<sanitized-branch>` beside it. Bare kills the "one branch is the checkout" special case,
and sanitizing kills the directory collision between a repo and a branch named after it. Because
the repo is bare, **the bare repos are the worktree registry** (`git worktree list`), which is what
keeps `session list` and `session rm` from disagreeing about what a session is.
_Avoid_: project dir, code dir.

**Orphan**:
A repo the box still holds under the workspace root that the blueprint no longer declares.
`workspace converge` **reports** every orphan and deletes nothing: the directory holds a bare repo,
its worktrees, and possibly unpushed work, so reclaiming it is a verb the operator runs themselves
(ADR-0010). This is deliberately the opposite of the staged config tree's prune rule, which
reclaims bytes smith wrote and can write again.
_Avoid_: stale repo, dangling repo.

**Placement**:
The primitive that puts a file on the box: `source-reference → dest-path [converge|once]`.
The source resolves to the referenced bytes exactly as they are, never trimmed, unlike an `env` value, whose `env:` and `file:` references are trimmed and whose `literal:` is taken verbatim.
`converge` (default) whole-file replaces on every pass; `once` writes only if absent, and the
worktree owns it thereafter. Scope is set by declaration site — under a `repos[]` entry it is
worktree-relative and materializes on session start, at top level it is an absolute box path and
materializes at `machine setup`. File-level only in v1.
_Avoid_: template, sync, copy.

**Toolchain selection**:
How a box gets its runtimes: `apt` installs the unversioned OS substrate, **`mise`** ensures the
blueprint's version-pinned `tools`. smith declares versions by writing config files it **fully
owns** and then running `mise install` — it never runs `mise use`, which would write into the
operator's own `~/.config/mise/config.toml`. Box-level `tools` and `env` land in
`~/.config/mise/conf.d/smith.toml`; per-repo overrides land in `~/workspace/<repo>/mise.toml`,
**above every worktree**, so smith never dirties a checkout and a generated file holding resolved
secret values cannot be committed by accident. Invocation is **`mise exec`, never `mise
activate`** — activate does not fire in the non-interactive and daemon-started shells smith and
agents run in.
_Avoid_: version manager, runtime install.

**Nearer wins**:
The precedence rule over toolchain config: the more specific declaration outranks smith's
generated one, in both directions. A `mise.toml` committed *inside* a repo beats the per-repo file
smith generates above the worktrees — the repo knows its own toolchain better than the blueprint
does. And because mise reads `conf.d/*.toml` at **lower** precedence than `~/.config/mise/config.toml`,
a hand-written global config on the box outranks smith's fragment. Both are deliberate: the
operator can always override without fighting a generated file. It is the one place an on-box file
silently outranks the blueprint, so a surprise about which version is active starts here
([issue #123](https://github.com/byranZA/smith/issues/123)).
_Avoid_: override, merge (nothing is merged — each file is whole-file owned).

**Execution**:
The unit a branch and worktree correspond to: 1..N tasks run sequentially on one branch, commits
landing directly on it. A single task is an execution of length one. Names are derived by whatever
drives the work, never by the session primitive — which takes `(repo, branch, base)` and knows
nothing of specs or tasks.
_Avoid_: job, run, task (a task is one item *within* an execution).

**Work-state predicate**:
The per-worktree readout defined once and used twice: **dirty** (`git status --porcelain`, minus
smith's own placement paths), **live** (`tmux has-session`), and an **unpushed** count (commits not
reachable from any remote-tracking ref). `session rm` gates on dirty and live — and on nothing
else; `session list` displays all three
([ADR-0010](./docs/adr/0010-session-teardown-safety-and-the-work-state-readout.md)).
_Avoid_: status, cleanliness.

**Driver**:
Who acts in a session — a human at a terminal, or a coding agent. Orthogonal to attach mode: an
agent-driven session can be observed or, when it needs a hand, jumped into. The driver process is
the tmux session's **root process** — the first process tmux starts in it, running as the smith
user like everything else in the session; nothing to do with the root user — so its exit ends the
session and session-end *is* the completion signal; smith reports liveness only, never the
driver's exit status, and never sends it keystrokes. **v1 creates human-driven sessions only.**
The **loop** and its **agent adapters** exist, but run in the repo they are invoked from rather
than as a session's root process, so `--driver agent` is still deferred.
_Avoid_: actor, runner, operator (reserved for the human running smith).

**Attach mode**:
How the operator connects to a session: read-only (observe) or writable (interact). Observation
and takeover are the same `tmux` session at two access levels — the mechanism behind watching an
agent work and stepping in. Read-only is **advisory**, not enforced: it is `tmux attach -r`, so
anyone who can SSH as `smith` can attach writable regardless. It guards against accidentally
typing into an agent's session — it is not a permission boundary
([ADR-0005](./docs/adr/0005-terminal-rides-the-ssh-door.md)).
_Avoid_: view mode, permission.

### The loop

**Loop**:
The driver that works one **execution** to completion, one **task** per iteration: it asks the tracker which task is next, hands exactly that task to an agent, and checks the tracker afterward to see whether the agent finished. The loop, not the agent, decides when it is done, and it exits when no task is left for an agent to pick up. It is one verb that runs wherever smith runs, in the repo it is invoked from. Locally that is the operator's machine acting as its own box. On a box, an agent-driven session's root process will be this same verb, never a reimplementation of it ([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)). v1 runs an execution's tasks one after another, in the current working tree on the current branch, and never creates or switches branch. After each task the agent closes, the loop itself pushes that branch to `origin` under the same name, never forcing and never pushing `origin`'s default branch, and a failed push stops the run, so closed work is never left on one machine only.
_Avoid_: ralph, runner, orchestrator, delegation loop (the old name for it).

**Spec**:
The tracker item an execution is derived from: a PRD whose children are the execution's tasks, in the order the spec lists them. The loop never hands a spec itself to an agent.
_Avoid_: PRD (as the domain term), epic, parent issue.

**Task**:
One child of a spec, and one iteration of the loop: an agent gets exactly one task per run. A task is meant either for an agent or for a human, and it is *available* when it is open, meant for an agent, and every task blocking it is closed. A task the agent leaves open is a failed attempt, and after a bounded number of them the loop skips the task rather than spinning on it.
_Avoid_: ticket, job, step.

**Tracker**:
Where specs and tasks live and where the loop reads their state. v1 knows exactly one tracker, GitHub Issues through the `gh` CLI, and uses `gh`'s own authentication, so smith still holds no credential. Knowing GitHub's issue model by name is a deliberate exception to *credential-agnostic* that covers the loop alone and sits behind one seam ([ADR-0012](./docs/adr/0012-the-repo-file.md)).
_Avoid_: forge (the hosting service, not its issue model), backlog, board.

**Agent adapter**:
smith's built-in knowledge of one coding-agent CLI (`claude`, `codex`, `pi`): how to run it unattended or attached to a terminal, and how to pass it a model and an effort. Unlike the provider adapter it is **code, not data**, because there are only a few agent CLIs and what separates them is behaviour, not configuration. Configuration names an adapter and never describes one. An unattended run is always fully autonomous, so there is no permission setting to configure ([ADR-0012](./docs/adr/0012-the-repo-file.md)).
_Avoid_: backend, provider (reserved for the provider adapter), agent plugin.

**Effort**:
smith's own small scale for how hard an agent should think, which each agent adapter translates into its CLI's flag. An adapter whose CLI cannot express effort refuses it by name. A **model**, by contrast, is passed to the agent verbatim, because model names change faster than smith releases.
_Avoid_: thinking level, reasoning effort (each agent's own name for it).

**Repo file**:
`.smith/repo.yaml` at a repo's root: the repo's own, committed half of smith's configuration, holding what anyone running the loop on that repo should share (agent, model, effort). It is optional and strictly validated, and it sits between a flag and the built-in default in precedence. It is the one per-repo layer, an amendment to the single config home, and is distinct from both the config home (the operator's) and box state (a box's). Where the repo's `.smith/` *is* the config home, as in a home directory kept in git, smith refuses rather than mixing them ([ADR-0012](./docs/adr/0012-the-repo-file.md)).
_Avoid_: project config, repo config, local config, config file.

**Loop prompt**:
The prompt the loop hands an agent for one task, with the task filled in through placeholders; the agent finds the spec from the task itself. smith ships a built-in one and uses it unless the repo has **ejected** its own copy to `.smith/prompt.md`. A repo that never ejects follows smith's latest default on every upgrade. A repo that has ejected owns the file, which smith never overwrites, and catches up by diffing against the printed built-in.
_Avoid_: system prompt, template (reserved for an adapter's command lines).
