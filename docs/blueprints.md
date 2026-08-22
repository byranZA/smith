# Blueprints and preferences

A **blueprint** declares what kind of box smith builds: which repos it carries,
which runtimes it installs, which files land where, how it is reached. It is
desired state — you re-run setup and smith converges the box to it — and one
blueprint instantiates many boxes.

**Preferences** are the other half: what belongs to *you* rather than to a kind
of box.

Both are optional. Everything smith reads from them can still come from a flag
or fall back to a built-in default.

Two worked examples ship with smith and are checked by its test suite, so they
cannot drift out of the schema:

- [`docs/examples/blueprints/acme.yaml`](./examples/blueprints/acme.yaml)
- [`docs/examples/preferences.yaml`](./examples/preferences.yaml)

They are the fastest way in: copy them, change what is yours, run
`smith blueprint check`.

## The config home

Everything lives under `~/.smith/`:

```
~/.smith/
  preferences.yaml      # yours, across every box
  blueprints/
    acme.yaml           # selected as `acme`
    prod.yaml
  cache/                # machine-local, gitignored
```

The directory is meant to be committed and shared — that is why no secret value
is ever written into it (see [References, not
values](#references-not-values) below). `cache/` is machine-local and stays out
of the commit.

A blueprint's **name is its filename without the extension**. `acme.yaml` on
disk is the blueprint `acme`, and smith refuses `acme.yaml` as a name rather
than accepting two spellings of one blueprint.

You can also keep a blueprint outside the config home and name it by path.
Anything containing `/`, or starting with `~` or `.`, is a path used verbatim;
anything else is a name resolved under `~/.smith/blueprints/`.

## Which half does a value belong in?

The separating test: **if you built a completely different box tomorrow, should
this value follow?**

Your provider account follows. The name you commit under follows. A list of
repos does not.

Mechanically, preferences hold **fixed-key fields only** — `access`,
`terminal`, `workspace`, `provider`, `git`. The open-ended collections —
`repos`, `placements`, `packages`, `tools`, `env` — are **blueprint-only**, and
writing one into `preferences.yaml` is an error.

The reason is reproducibility. A box records which blueprint it was built from
and records nothing about your preferences, so the blueprint pointer has to be
enough to rebuild the box. If repos could come from preferences, a teammate who
clones your `~/.smith` and builds the same blueprint could get a different set
of repos while both boxes truthfully report the same blueprint.

## Precedence

Every field resolves down one chain, most specific first:

**CLI flag → blueprint field → preference → built-in default**

It is resolved **per field**. A blueprint pinning `access` does not wipe the
`workspace` your preferences set.

The built-in defaults are `access: public`, `terminal: tmux`,
`workspace: ~/workspace`.

### The provider block replaces wholesale

One field breaks the per-field rule. A **provider block is a coherent
description of one provider's CLI** — its `create`, `list` and `destroy`
templates, the environment it `requires`, how it stamps and reads back a
marker, how identity is extracted from its JSON. Merging it field by field
would hand one provider's `requires` and `extract` to another provider's
`create`, and the mismatch would surface only when you tried to create a box.

So: a blueprint that names `provider` **at all** replaces the preference block
entirely, and a blueprint silent on `provider` inherits it entirely. Nothing is
taken from both.

## Checking before you build

```sh
smith blueprint check              # validate ~/.smith/preferences.yaml alone
smith blueprint check acme         # validate preferences + the blueprint `acme`
smith blueprint check ./team.yaml  # validate a blueprint kept outside ~/.smith
```

`check` writes nothing, touches no box, and needs no network — it answers *what
would setup actually do?* before any box exists. Exit 0 valid, exit 1 invalid or
not found; the resolved configuration goes to stdout and any refusal to stderr,
so the two compose separately.

The success case is the resolved configuration, with where each value came
from:

```
$ smith blueprint check acme
blueprint "acme" is valid (/home/ada/.smith/blueprints/acme.yaml)

resolved configuration:
access:     tailscale (blueprint)
terminal:   tmux (blueprint)
workspace:  ~/workspace (blueprint)
provider:   hcloud (blueprint, replacing the preference)
git:
  user_name:  Ada Lovelace (blueprint)
  user_email: ada@acme.example (blueprint)
repos:
  acme-api (git@github.com:acme/api.git)
    base: develop
    tools:
      node: 22
    env:
      ACME_DB_PASSWORD: file:~/.secrets/acme/db-password
      ACME_REGION: literal:eu-central
    placements:
      file:~/.secrets/acme/api.env -> packages/api/.env (converge, 0600)
      file:~/fixtures/acme.sql -> seed.sql (once, 0644)
  web (git@github.com:acme/web.git)
    base: main
    placements:
      file:~/.secrets/acme/web.env -> .env.local (converge, 0600)
packages:
  ripgrep
  jq
  postgresql-client
tools:
  go: 1.23
  node: 20
env:
  ANTHROPIC_API_KEY: file:~/.secrets/acme/anthropic-key
  GITHUB_TOKEN: env:GH_TOKEN
  LOG_LEVEL: literal:debug
placements:
  file:~/.secrets/acme/id_forge -> ~/.ssh/id_forge (converge, 0600)
  file:~/.config/acme/agent-instructions.md -> ~/.config/acme/agent-instructions.md (once, 0644)
```

Fixed-key fields carry the origin of their value; the collections are
blueprint-only, so they are printed as the blueprint declared them. A repo
appears under the name it resolves to — its own, or the last segment of its
clone URL — which is the directory it lands in under the workspace. A field
nobody declared and smith has no default for, such as a git identity, is left
out rather than shown empty.

`--access public|tailscale` overrides both the blueprint and your preferences,
which is how you see the top of the precedence chain at work:

```
$ smith blueprint check acme --access public
...
access:     public (flag)
```

The failure case is every problem in the document at once, in the order they
appear, so you fix the file in one pass rather than one line per run:

```
$ smith blueprint check ./bad.yaml
./bad.yaml: blueprint is invalid:
  line 2: unknown field "terminals"
  access: "wireguard" is not recognised here: smith knows "public" and "tailscale"
  repos[0]: a repo needs a url naming the remote to clone
```

## Validation is strict

A key the schema does not define is an **error**, not a warning. A desired-state
document that quietly ignores what you wrote is the worst available failure
mode: nothing tells you the field did nothing.

Three further rules sit on top of the structural pass. Each closes a hole that
otherwise fails *silently* — the reasoning is recorded in
[ADR-0006](./adr/0006-the-blueprint-config-surface.md):

- **Schema field names are reserved inside `tools` and `env`.** A misindented
  `base:` or `url:` would otherwise become a phantom tool or environment
  variable. The rule is case-sensitive, so a real environment variable named
  `URL` or `MODE` is untouched.
- **A placement destination must match the scope it was declared in.** A
  box-scoped placement writes an absolute or `~`-relative path; a repo-scoped
  one is worktree-relative.
- **Every value naming a secret must parse as a known reference scheme.**

## References, not values

A blueprint points *at* a secret; it never contains one. Every `env` value and
every placement `from` is a `scheme:argument` reference:

| Reference      | Means                                                   |
| -------------- | ------------------------------------------------------- |
| `env:VAR`      | read `VAR` from the environment smith runs in            |
| `file:PATH`    | read the contents of `PATH` on your machine              |
| `literal:TEXT` | the text itself                                          |

A value with no scheme is refused, and an unknown scheme is named as unknown —
`LOG_FORMAT: "json:pretty"` fails as an unknown scheme rather than being taken
as a literal. `literal:` is not valid as a placement source.

This does not stop you committing a secret. It makes doing so **deliberate**,
and makes `grep -r 'literal:'` a complete audit.

## smith knows no service by name

smith **places files and exports environment variables**. That is the entirety
of its credential story. It knows nothing about GitHub, GitLab, Anthropic or
npm, and it never will — the moment it knew about GitHub it would owe GitLab,
`gh` version skew, and a choice between auth modes.

The concrete consequence, worth reading twice:

> A blueprint that declares `GITHUB_TOKEN: env:GH_TOKEN` and clones over
> `https://github.com/acme/api.git` **will not authenticate.** git does not read
> that variable; it needs a credential helper configured to use it. smith will
> not notice, will not warn, and the clone will fail on the box.

Two things that do work:

- **Clone over SSH** (`git@github.com:acme/api.git`) and place the key with a
  box placement to `~/.ssh/id_forge`, `perms: "0600"`. This is what the example
  blueprint does.
- **Place a credential helper's own config** — for example a
  `~/.git-credentials` file, or a `~/.gitconfig` wiring up a helper — with a box
  placement, and clone over `https://`.

Either way the declaration is yours and it is visible in the blueprint. The
first-run cost is real, and the example is how it is paid.

## House style

Blueprints are written **uniformly expanded**: one key per line, list entries
written out rather than folded into inline `{ from: ..., to: ... }` maps. Octal
`perms` are quoted, so YAML keeps `"0600"` a string.

This is presentation only. YAML parses both spellings identically and smith
cannot enforce it, which is exactly why the examples and this page are what
carry it.
