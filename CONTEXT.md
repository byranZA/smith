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

**Bootstrap-in path**:
The one-time public-SSH entry smith uses to reach a brand-new box — `smith machine setup
<login>@<host>` over system `ssh`/`scp`, authenticating with the key the provider already
injected. Distinct from the ongoing `ssh smith@host` reach the setup establishes.
_Avoid_: onboarding, first connection.

**Bootstrap login**:
The root-or-sudo-capable account the operator connects *as* during the bootstrap-in path.
Under a non-root login smith runs via `sudo` and fails early if passwordless sudo is absent.
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
The one pluggable choice layered over the base: how the box is reached. `--access=public`
(default) leaves hardened SSH open on the public IP; `--access=tailscale` joins a tailnet and
closes public SSH. Architected as a swappable layer so neither mode re-architects the other.
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
One step of `machine setup`'s admin-side pipeline, driven from local smith over SSH rather than by
`bootstrap.sh` — the tailscale access layer, the smith-install stage, the config-staging stage, the
workspace stage. Unlike a phase, a stage is not in the marker's `completed_phases` and does not
touch the base layer. The install and staging stages must precede the workspace stage, because
on-box smith is what runs it ([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)).
_Avoid_: phase (reserved for the base-layer sequence), step.

**Marker**:
The versioned on-box record of what bootstrap did — `/etc/smith/bootstrap.json` (schema + smith
version + access mode + completed phases + timestamp, plus the box's self-record: which blueprint
it was built from, the operator-chosen box name, and the provider destroy reference to tear it
down by hand). Records **nothing** about the installed smith binary or the create-time SSH key —
both are probed, not stored. Lets `smith machine status`
read state back, any admin machine re-run idempotently, and smith recognise a box from just its IP. A **ledger, not a gate**: every `setup` run
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
mismatch**; it is refused, never warned. Independent of schema skew
([ADR-0008](./docs/adr/0008-smith-runs-on-the-box.md)).
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
placement/`env` path, resolved operator-side at `machine setup`, and **sits on the box in
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
`~/.smith/` is the **operator's** config home — `blueprints/`, `preferences`, and a gitignored
`cache/` holding the box inventory. `/etc/smith/` is a **provisioned box's** state — the marker,
the staged blueprint, the staged placement bytes, and the staged env. Separate locations and roles, same format;
they must never share a directory, because one machine could one day hold both. The config home is
created by the **first write** into it and never by a read, and its `.gitignore` is appended to
rather than rewritten: the file is the operator's.
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

**Preferences**:
`~/.smith/preferences.yaml` — the operator's own half of the config home, answering *if you built
a completely different box tomorrow, should this value follow?* with yes. **Fixed-key fields
only** (`access`, `terminal`, `workspace`, `provider`, `git`); the open-ended collections
(`repos`, `placements`, `packages`, `tools`, `env`) are blueprint-only, so the marker's blueprint
pointer is enough to reproduce a box. Optional, like the blueprint itself
([ADR-0006](./docs/adr/0006-the-blueprint-config-surface.md)).
_Avoid_: settings, defaults, user config, global config.

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
_Avoid_: provider-agnostic (reserved for the provider adapter), integration.

**Box inventory**:
`~/.smith/cache/boxes.json` — a name → proven-target address book and nothing else. An entry is an
operator-chosen name plus the opaque SSH target smith **proved** works. Identity only: access mode,
phases, blueprint pointer and destroy reference all stay marker-side, so there is nothing in it
that can silently go stale — hence no TTL and no `last_seen`. Written by a successful `machine
setup` and by `machine add`, which is read-only towards the box; `machine list` is instant and
offline (`--probe` reports reach without storing it); only `machine forget` removes. **Rebuildable,
not self-rebuilding**: a deleted file loses nothing unique, but v1 has no discovery path, so it is
rebuilt by hand, one `machine add` per box
([ADR-0011](./docs/adr/0011-the-box-inventory-is-identity-only.md),
[docs/inventory.md](./docs/inventory.md)).
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
the tmux session's **root process**, so its exit ends the session and session-end *is* the
completion signal; smith reports liveness only, never the driver's exit status, and never sends it
keystrokes. **v1 creates human-driven sessions only** — `--driver agent` waits for the delegation
loop, since with no agent adapter there is nothing to launch.
_Avoid_: actor, runner, operator (reserved for the human running smith).

**Attach mode**:
How the operator connects to a session: read-only (observe) or writable (interact). Observation
and takeover are the same `tmux` session at two access levels — the mechanism behind watching an
agent work and stepping in. Read-only is **advisory**, not enforced: it is `tmux attach -r`, so
anyone who can SSH as `smith` can attach writable regardless. It guards against accidentally
typing into an agent's session — it is not a permission boundary
([ADR-0005](./docs/adr/0005-terminal-rides-the-ssh-door.md)).
_Avoid_: view mode, permission.
