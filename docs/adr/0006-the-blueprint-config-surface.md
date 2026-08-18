# The config surface arrives — the Blueprint in `~/.smith/`

## Context

[ADR-0003](./0003-no-local-config-file-in-v1.md) deliberately refused a local config file. The
line it drew was a domain distinction: the setup domain has only **operational** secrets — one
Tailscale key, consumed then discarded — which one flag handles, while **provisioned** config
(repos, env, seed data, toolchain: many items, each needing a name, a destination, and a source)
belongs to a later map. That map has now arrived.

Standing up a dev box needs a declaration of *what kind of box*: which repos to clone, which
runtimes to install, which credentials to place where, which provider to create it at. This is
irreducibly a set of collections, and flags cannot carry it — a third repo means a third
`--repo`, a fourth placement a fourth `--place`, and nothing records the whole for the next box.
It is also *desired state*: the operator re-runs `machine setup` and expects convergence, which
requires something durable to converge *to*.

That forces a config surface, and with it four questions ADR-0003 was able to defer: where it
lives, what shape it has, what syntax carries it, and what is committed versus machine-local.

## Decision

**smith gains one config home — `~/.smith/` — holding YAML documents the operator commits and
shares.** The [Blueprint](../../CONTEXT.md) is the unit: the reusable declaration of a *kind* of
box, distinct from the marker, which records what a *particular* box did.

```
~/.smith/
  .gitignore            # written by smith; contains `cache/`
  preferences.yaml      # operator-wide, fixed-key fields only
  blueprints/
    acme.yaml           # `--blueprint acme`; the marker records `acme`
    prod.yaml
  cache/                # machine-local, gitignored
    boxes.json          # name → proven SSH target
    placements/         # resolved bytes, staged for the box
```

**The blueprint stays optional.** ADR-0003's flag path survives intact — a box can still be
provisioned with flags alone, and preferences then sit alone between the flag and the built-in
default. Precedence is **field-level**: CLI flag > blueprint field > home preference > built-in
default.

### One home, not a layered search path

There is no per-project, per-directory, or per-repo config layer. A single canonical `~/.smith/`
is the whole search. smith never runs `git` to discover a repo root and never walks up from the
working directory, so *where you are standing* has no bearing on what a command does — the box
name and the blueprint name are the only inputs.

### The blueprint / preferences split

Both live in `~/.smith/`, both are committed, both are trusted identically. They differ in what
they are a property *of*: a blueprint is a property of a **kind of box** ("an acme dev box has
these repos, node 20"), preferences are a property of the **operator**, across every box ("we're
a Hetzner shop and we always run tailnet"). The separating test: *if you built a completely
different box tomorrow, should this value follow?*

**Preferences take fixed-key fields only; open-ended collections are blueprint-only.**

| Preference-settable | Blueprint-only |
|---|---|
| `access`, `terminal`, `workspace`, `provider` | `repos`, `placements`, `packages`, `tools`, `env` |

The reason is **reproducibility, not merge semantics**. The marker records which blueprint a box
was built from and records nothing about preferences, so the blueprint pointer must be enough to
reproduce the box. If `repos` could come from preferences, a teammate who clones `~/.smith` and
runs `machine setup prod` could get a different set of repos while both boxes truthfully record
`blueprint: prod` — the pointer would be a lie. The merge ambiguity (does a blueprint's list
replace or append to a preference's?) is a symptom of the same thing.

The rule is mechanical rather than an allowlist, so it answers schema growth in advance: the AFK
map's future `agent` block is fixed-key command references and lands on the left automatically.

### The format is YAML — one format, one file per blueprint

`~/.smith/blueprints/<name>.yaml`. **The extension is not part of the name**: `--blueprint acme`
resolves `acme.yaml`, and the marker records `acme`.

**Validation is strict — an unknown key is an error, not a warning.** A typo in a desired-state
config that quietly does nothing is the worst available failure mode. "Open to a future `agent`
block" means *the key is added when the AFK map ships*, never *anything is silently swallowed*.

Two validation rules sit on top of the strict pass. Both close **silent-success** holes that
strict parsing cannot see, and both will read as over-strict to anyone who does not know what
they caught:

**1. Schema field names are reserved inside the open maps.** `tools` and `env` are open maps, so
a key that lands in the wrong one parses clean:

```yaml
    tools:
      node: "22"
      base: develop     # meant repo-level, one indent too deep
```

That stands up a box with no `base` and a phantom tool, and the operator finds out when
`session start` cuts from the wrong branch. A key named `url`, `base`, `mode`, `from`, `to`,
`name`, `perms`, `repos`, `placements`, `access`, `terminal`, `workspace`, `provider`,
`packages`, `tools` or `env` under `tools`/`env` is therefore an error. **The match is
case-sensitive**, which is what makes the rule free: schema fields are lowercase, so a real env
var named `URL` or `MODE` is untouched and only the misindent shape trips.

**2. A placement's `to:` must match its declaration scope** — repo-scoped is worktree-relative,
box-scoped is absolute or `~`-relative. Scope is a property of *where the placement is declared*,
which is unambiguous to the parser but not to a reader twenty lines below the `repos:` header.
Without the rule, a repo placement writing `to: ~/.npmrc` lands silently inside the worktree
where the app never reads it.

**House style is uniform expansion** — placements are written out like every other block rather
than as inline `{ from, to, mode, perms }` maps. This is presentation only: YAML parses both
identically and smith cannot enforce it. It binds the documentation and any future scaffold.

### The cache is not config

`~/.smith/cache/` is machine-local and gitignored, and holds two things that are *derived*, never
authored: the box address book (`boxes.json` — an operator-chosen name mapped to an SSH target
smith has **proven** works, identity and nothing more) and resolved placement bytes staged for
the box. smith writes the `.gitignore` itself. The cache is rebuildable but **not
self-rebuilding** in v1: nothing in it is unique to it, but reconstruction is one `machine add`
per box, by hand.

Because a box knows of no other boxes, **on-box smith carries no cache at all**; `list`, `add`
and `forget` are the only local-only verbs.

## Considered options

- **Stay on flags (ADR-0003 unchanged)** — rejected. Collections are the whole point of a
  blueprint, and flags cannot express a set of repos each with its own placements without one
  flag per element and no durable record. ADR-0003 was right for the setup domain and is amended,
  not overturned: its reasoning ("the provisioning surface belongs to a later map") anticipated
  exactly this.
- **TOML** — rejected, but *not* on the grounds this decision expected. TOML nests
  `repos[].placements[]` perfectly well via `[[repos.placements]]`; the anticipated awkwardness is
  real but costs density (75 lines against 55 for the same box), not expressiveness. It lost on
  the **error surface**: with the same schema and the same broken document, `yaml.v3`'s
  `KnownFields(true)` reports *every* unknown key *with a line number*, while BurntSushi's
  `MetaData.Undecoded()` reports key paths with **no position** and collapses the array index —
  `repos.ref` does not say *which* repo. That is a property of the Go libraries rather than of
  the formats, but it is the property an operator meets, and strict validation is the feature the
  surface rests on. Recorded because the instinct to revisit the format will reach for nesting
  arguments; the answer is line numbers.
- **Supporting both formats** — rejected, and it was the source of every layering problem.
  Selecting by bare name, `--blueprint prod` finds both `prod.yaml` and `prod.toml`, needs a
  precedence rule no operator can predict, and leaves a marker recording `blueprint: prod` no
  longer identifying a file. One format dissolves all three.
- **Directory-per-blueprint** (`blueprints/acme/blueprint.yaml`) — rejected. It earns a second
  layout to explain only once a blueprint has sibling files to hold, and every candidate sibling
  (the provider adapter, seed data) is currently a reference to a path *outside* `~/.smith/`.
- **Per-project or per-repo config layers** — deferred, not rejected. A managed repo declaring
  its own toolchain devcontainer-style has real value, but it adds a merge dimension and a trust
  dimension (the repo's config is not the operator's), and it makes the answer depend on the
  working directory. The blueprint stays the single declaration site for v1.
- **Checking tool names against `mise`** instead of reserved names — rejected. It needs a lookup
  at validate time and a story for tools mise has not heard of, to catch the same class the
  reserved list catches offline.

## Consequences

- **ADR-0003 is amended, not overturned.** The flag path survives and stays first in precedence.
  What changes is that flags are no longer the *only* surface.
- **[ADR-0002](./0002-secrets-are-references-not-values.md)'s `scheme:arg` primitive is now the
  blueprint's value grammar**, exactly as ADR-0003 predicted — `env:`/`file:` in `env` maps and
  placement sources, additively, with no rewrite. The blueprint hard-errors any value that is not
  a known scheme; `literal:` is not valid in a placement `from`.
- **The marker's blueprint pointer is load-bearing**, which is what forces the preferences
  field-set rule above. Anything that lets a collection reach a box from outside the blueprint
  breaks the pointer's promise, however convenient it looks.
- **Strict validation is a feature, not a strictness setting.** Downgrading unknown keys to
  warnings would restore precisely the failure mode the surface exists to prevent. The two extra
  rules exist because strict parsing alone is blind wherever the schema is open, and removing
  them as pedantic re-opens holes that fail silently rather than loudly.
- **The config home is shareable by construction.** `blueprints/` and `preferences.yaml` are
  committed and shared; the boundary between them and machine-local state is a `.gitignore` line
  smith writes, not a convention the operator maintains.
- **Nothing here is secret-bearing at rest by design, but the staged cache is.** Resolved
  placement bytes sit in `cache/placements/` in plaintext; that is a deliberate, separately
  recorded non-goal and not a property of this ADR.
- **Anyone adding a config location re-opens this decision.** A `./smith.yaml`, a `$SMITH_CONFIG`
  search path, or a per-repo overlay each reintroduces the working-directory dependence and the
  merge dimension this ADR removed, and must be weighed against them rather than slipped in
  beside.

See [issue #58](https://github.com/byranZA/smith/issues/58),
[issue #62](https://github.com/byranZA/smith/issues/62),
[issue #65](https://github.com/byranZA/smith/issues/65) and
[issue #69](https://github.com/byranZA/smith/issues/69).
