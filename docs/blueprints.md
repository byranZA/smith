# Blueprints and preferences

A **blueprint** declares what kind of box smith builds: which repos it carries,
which runtimes it installs, which files land where, how it is reached. It is
desired state — you re-run setup and smith converges the box to it — and one
blueprint instantiates many boxes.

**Preferences** are the other half: what belongs to *you* rather than to a kind
of box.

Both are optional. Everything smith reads from them can still come from a flag
or fall back to a built-in default.

Worked examples ship with smith and are checked by its test suite, so they
cannot drift out of the schema:

- [`docs/examples/blueprints/acme.yaml`](./examples/blueprints/acme.yaml)
- [`docs/examples/blueprints/loop-box.yaml`](./examples/blueprints/loop-box.yaml) — the smallest box that clones a private repo and runs the loop
- [`docs/examples/preferences.yaml`](./examples/preferences.yaml)
- [`docs/examples/adapters/`](./examples/adapters/) — provider adapters on
  their own, explained field by field in [Provider adapters](./providers.md)

They are the filled-in reference. The fastest way in is `smith init`, which
writes commented **starters** into the config home — `preferences.yaml` and a
blueprint named `personal` — valid as written and never overwriting a file
already there. Fill them in, borrowing from the examples, and run
`smith blueprint check personal`.

## The config home

Everything lives under `~/.smith/`:

```
~/.smith/
  preferences.yaml      # yours, across every box
  blueprints/
    acme.yaml           # selected as `acme`
    prod.yaml
  cache/
    boxes.json          # the box inventory: machine-local, gitignored
```

The directory is meant to be committed and shared — that is why no secret value
is ever written into it (see [References, not
values](#references-not-values) below). `cache/` is machine-local and stays out
of the commit; it holds the [box inventory](./inventory.md), the name → address
book smith writes when a setup succeeds.

smith creates `~/.smith/` on its **first write** into it — `smith init`, or
registering a box — and never on a read: `blueprint check` validates without creating anything. The
creation also puts `cache/` into the config home's `.gitignore`, **appending**
to a `.gitignore` you already keep rather than rewriting it.

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
`repos`, `placements`, `packages`, `groups`, `tools`, `env` — are **blueprint-only**, and
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

`terminal` is a one-value choice: `tmux` is the only value, and the terminal every session runs under. Any other value is refused by `blueprint check`.

### Every resolved field reaches the box

Given `--blueprint`, `machine setup` resolves the chain on your machine and stages the result onto the box as the **staged resolution**, `/etc/smith/resolved.json`, beside the verbatim blueprint (see [Config staging](./on-box.md#config-staging)). On-box smith reads `access`, `terminal`, `workspace` and the git identity from there and resolves nothing itself, so the box acts on exactly what `blueprint check` showed you, including a `workspace` set only in your preferences. `provider` is not staged: it is `machine create`'s concern, resolved on your machine. A changed field reaches the box on the next `machine setup`.

Changing `workspace` changes where clones and sessions land from then on. The clones under the old root stay where they are, and nothing reports them: the orphans step scans only the current root.

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

### The adapter itself

The rest of the block — the three templates, the four placeholders smith
substitutes, `requires`, `ssh_key`, `marker`, `record` and `extract` — is one
page of its own: [Provider adapters](./providers.md). It covers what an adapter
is, that it is optional, and the worked examples in
[`docs/examples/adapters/`](./examples/adapters/): two verified against a real
provider (both DigitalOcean) and one, `hcloud`, explicitly not.

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
every reference it declares resolves on this machine

resolved configuration:
access:     tailscale (blueprint)
terminal:   tmux (blueprint)
workspace:  ~/workspace (blueprint)
provider:   doctl (blueprint, replacing the preference)
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
  docker.io
  docker-compose-v2
groups:
  docker
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

check also resolves every `env:` and `file:` reference the blueprint declares — each placement source and env value, at the box and in each repo — exactly as `machine setup` will, and says whether they all resolve on this machine. When some don't, it lists each one with its scope and the reason, as setup's refusal would, and prints no value:

```text
$ smith blueprint check acme
blueprint "acme" is valid (/home/ada/.smith/blueprints/acme.yaml), but `smith machine setup` from here would refuse it:
2 reference(s) will not resolve on this machine:
  ~/.ssh/id_forge from file:~/.secrets/acme/id_forge: read what this placement source names: read secret file "~/.secrets/acme/id_forge": open /home/ada/.secrets/acme/id_forge: no such file or directory
  GITHUB_TOKEN from env:GH_TOKEN: read what this blueprint value names: environment variable "GH_TOKEN": secret reference resolved to nothing
```

An unresolved reference still exits 0: a blueprint is also checked where no box will be set up from it, such as CI or a teammate's machine, and there `file:~/.secrets/…` never resolves. `literal:` values are not references and are never listed among them; an empty one, `literal:` with nothing after it, is listed under its own heading, `literal value(s) declare nothing`, since setup refuses it too.

Fixed-key fields carry the origin of their value; the collections are
blueprint-only, so they are printed as the blueprint declared them. A repo
appears under the name it resolves to — its own, or the last segment of its
clone URL — which is the directory it lands in under the workspace. A field
nobody declared and smith has no default for, such as a git identity, is left
out rather than shown empty. The one exception is `provider`, which reads
`none declared`.

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

- **Each `groups` entry must be a group name, declared once.** A name follows Debian's `adduser` rule for a system group: lowercase letters, digits, `-` and `_`, starting with a letter or `_`, optionally ending in `$`. A repeated entry is refused by name.

One rule spans both files, so it is checked after precedence has run: **a git
identity and a box placement to `~/.gitconfig` are refused together**, because
both claim that file. Choose one and drop the other.

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

`file:` expands a leading `~/` to your home directory; `~user` is refused. A placement source is placed byte for byte, so a key keeps its final newline, while an `env` value read through `env:` or `file:` is trimmed of surrounding whitespace.

This does not stop you committing a secret. It makes doing so **deliberate**,
and makes `grep -r 'literal:'` a complete audit.

References are resolved **on your machine** when `machine setup --blueprint`
runs, and the values are staged onto the box in plaintext under `/etc/smith/`
(see [Config staging](./on-box.md#config-staging)). A changed value reaches the
box on the next `machine setup`, not on `workspace converge`.

## Groups

`groups` lists existing groups the smith user joins on the box, such as the `docker` group that lets it run `docker` without `sudo`:

```yaml
packages:
  - docker.io
  - docker-compose-v2
groups:
  - docker
```

- **A group comes from a package.** smith never creates one: a group the box does not hold fails the workspace's groups step, so declare the package that creates it.
- **Membership is add-only.** Dropping a group from the blueprint leaves the smith user in it, as dropping a package leaves it installed.
- **There is no denylist.** The smith user already has passwordless `sudo`, so no group grants it anything new.

Membership reaches new logins only, and a running tmux server keeps the groups it started with. [The workspace stage](./workspace.md#groups) says what that means for sessions.

## smith knows no service by name

smith **places files and exports environment variables**. That is the entirety
of its credential story. It provisions no credential for GitHub, GitLab,
Anthropic or npm — the moment it did for GitHub it would owe GitLab, `gh`
version skew, and a choice between auth modes. The one place smith knows a
service by name is [the loop](./loop.md), which reads GitHub Issues through
your own authenticated `gh` and holds no credential of its own
([ADR-0012](./adr/0012-the-repo-file.md)).

The concrete consequence, worth reading twice:

> A blueprint that declares `GITHUB_TOKEN: env:GH_TOKEN` and clones over
> `https://github.com/acme/api.git` **will not authenticate.** git does not read
> that variable; it needs a credential helper configured to use it. smith will
> not notice, will not warn, and the clone will fail on the box.

What does work is worked end to end in the next section. Whichever path you take, the declaration is yours and it is visible in the blueprint. The first-run cost is real, and the docs and examples are how it is paid.

## A box that can clone and run the loop

A box that runs [the loop](./loop.md) on a private repo needs three things from its blueprint: a way for git to clone and push, a token `gh` can read the tracker with, and the agent's own credential. [`docs/examples/blueprints/loop-box.yaml`](./examples/blueprints/loop-box.yaml) is the whole of it, filled in; this section says why each line is there.

### Default path: HTTPS and one token

One token covers the clone, the push and the tracker. The SSH path needs a key and GitHub's host keys on top of that same token.

- **The token** is a fine-grained personal access token on the blueprint's repos with **contents** and **issues**, both read/write. Add **pull requests** write only if a repo's own prompt has agents open PRs; the loop itself pushes work branches and never opens one. The loop only checks that it can read the tracker, so a token without write access first shows up at the first `smith:working` mark, not at setup.
- **Declare it as `GH_TOKEN`** in box `env`, the variable `gh` reads first. It can't be confused with a `GITHUB_TOKEN` your own shell already holds.
- **Never source it from `gh auth token`.** That puts your own broad token on the box in place of a narrow one chosen on purpose ([ADR-0009](./adr/0009-provisioned-secrets-sit-in-plaintext.md)).
- **`gh` is a box-level tool** (`gh: latest` under `tools`). The clone runs as `mise exec -- git …` before any repo's own tools are in scope, so a `gh` declared on a repo isn't there yet. smith installs neither `gh` nor any agent unless the blueprint declares it.
- **A placement wires git to `gh`.** git ignores `GH_TOKEN`; it needs a credential helper. Keep this file on your machine and place it at `~/.config/git/config`, which git reads beside `~/.gitconfig`. The [git identity](#validation-is-strict) owns `~/.gitconfig`, so a placement there is refused beside one.

  ```ini
  [credential "https://github.com"]
      helper =
      helper = !gh auth git-credential
  ```

  The empty `helper =` clears any helper set before it, so `gh` is the only one asked.

**Prefer `file:` over `env:`** for the token and every other secret. An `env:VAR` reference takes whatever `VAR` holds in your shell when `machine setup` runs, and that fails silently in both directions:

- **You forget to export the box's value.** If your shell already holds the variable for its own use, setup resolves *that* value with no error and stages it onto the box: usually a broader credential than the one meant for it.
- **You source a file to set the box's values.** That terminal now carries the box's credentials instead of yours, and later commands in it run as the wrong identity.

A `file:` reference depends on nothing in your shell, and the variable's name on the box is independent of anything on your machine. Keep `env:` for values you really mean to take from your current environment. The examples keep secrets under `~/.secrets/<blueprint>/`, outside the config home.

How the token reaches each shell on the box is in [What a shell on the box gets](./on-box.md#what-a-shell-on-the-box-gets): a session holds it, and a `git push` typed in a plain SSH shell gets it through the `gh` shim.

### The agent's credential

The loop's agent is a box-level tool with its own credential:

| Agent | `tools` key | Credential, and where it goes | How to get it |
| --- | --- | --- | --- |
| `claude` | `claude` | `CLAUDE_CODE_OAUTH_TOKEN` in box `env`; or `ANTHROPIC_API_KEY` | `claude setup-token` on your machine, for a subscription; or an API key from the Anthropic console |
| `codex` | `codex` | your login file, placed `from: file:~/.codex/auth.json` `to: ~/.codex/auth.json`, `mode: once`, `perms: "0600"`; or `OPENAI_API_KEY` in box `env` | `codex login` on your machine, for a subscription; or an API key from the OpenAI platform |

- **codex's login is placed with `mode: once`** because codex rewrites the file when it refreshes its token, and `converge` would overwrite that with your stale copy. The cost: setup never replaces the file, so to change the box's login, delete it on the box and run `machine setup` again.
- **Whether one codex login can be shared** between your machine and the box across a token refresh hasn't been checked. A box-only login avoids the question: `CODEX_HOME=~/.secrets/personal/codex codex login`, then place that directory's `auth.json` instead.
- **The model reaches whichever agent runs.** A repo file's `model: opus` is handed to codex as-is, so `--agent codex` needs its own `--model`.
- Other adapters, such as `pi`, read their own credential; declare it the same way.
- smith checks none of these. A bad credential first shows up when the loop's first agent fails.

### Alternative path: SSH

Clone over `git@github.com:…` instead, with two placements:

- **A dedicated key** at `~/.ssh/id_ed25519`, `perms: "0600"`. ssh tries that name by default; a key at any other name needs an `ssh_config` entry too.
- **GitHub's host keys** at `~/.ssh/known_hosts`, `mode: once`, or the first, non-interactive clone stops at the host-key prompt. Build the file from `ssh_keys` in `https://api.github.com/meta`, each line prefixed `github.com `, and check it against [GitHub's published fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints) rather than trusting a bare `ssh-keyscan`.

The loop still reads the tracker through `gh`, so this path needs the token and box-level `gh` as well. [`acme.yaml`](./examples/blueprints/acme.yaml) clones over SSH.

## House style

Blueprints are written **uniformly expanded**: one key per line, list entries
written out rather than folded into inline `{ from: ..., to: ... }` maps. Octal
`perms` are quoted, so YAML keeps `"0600"` a string.

This is presentation only. YAML parses both spellings identically and smith
cannot enforce it, which is exactly why the examples and this page are what
carry it.
