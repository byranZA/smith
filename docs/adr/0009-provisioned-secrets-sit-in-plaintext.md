# Provisioned secrets sit in plaintext on the box — one path in, and no at-rest protection

## Context

[`CONTEXT.md`](../../CONTEXT.md) has always distinguished an **operational secret** (consumed
during bootstrap and discarded — the Tailscale auth key) from a **provisioned secret** (installed
*onto* the box for later use), and deferred the second to a later map. This is that map.

A box now needs credentials of its own regardless of who is driving a session: a git identity,
forge auth, and a coding-agent API key. They are properties of the **box**, not of the driver
([ADR-0008](./0008-smith-runs-on-the-box.md)), so the question is not "how does an agent
authenticate" but "how does a credential get onto a box, and what shape is it in once it is
there".

The honest constraint frames the whole answer: the `smith` user has **passwordless sudo by
design** — a deliberate blast-radius tradeoff for a self-managing dev box. Anything that user can
read is readable by anything that becomes that user.

## Decision

**Credentials reach the box by the placement/`env` path the blueprint schema already defines** —
resolved operator-side at `machine setup`, staged on-box, converged on session start — **they sit
there in plaintext, and smith says so out loud.** Blast radius and box disposability are the
control; file permissions are a speed bump. The one credential fat enough to matter, the provider
token, never reaches a box at all.

### One acquisition path

The placement/`env` path is the **sole** mechanism in v1.

| | `env:` map | `placements` |
|---|---|---|
| Unit | one named variable | one whole file |
| smith resolves | the value | the source reference only |
| smith parses contents | n/a | **never** — whole-file byte replace |
| Lands as | exported variable (mise `[env]`) | a file at a path |

A whole `.env` file is **one placement line**, however many variables it holds; its contents are
entirely opaque to smith, so nothing inside it is enumerated, validated, or scheme-checked — only
the reference *to* it is.

Secret managers are not a second mechanism: `op:` / `keychain:` are *source* extensions in
[ADR-0002](./0002-secrets-are-references-not-values.md)'s clean-room `scheme:arg` slot, resolved
operator-side exactly like `file:` and `env:`. Additive whenever wanted.

### Minting on the box — a second shape, never in the setup path

`smith machine keygen` mints an ed25519 key **on the box**, idempotently — key exists → reprint
the existing public key, never remint — and prints the public key for the operator to register as
a forge deploy or account key. The private half is born on the box and never transits: nothing on
the laptop, nothing in the stage, nothing in a blueprint. This is proven machinery, not new
capability: the `ssh-hardening` self-test already shells out to `ssh-keygen` on the box for its
ephemeral probe key.

It is a **separate verb precisely so unattended setup stays possible.** As a blueprint field or a
setup phase it would either block on a human or mint a key nobody registers — and an unregistered
key is worse than no key, because the failure surfaces later as a clone error. Blueprints say
nothing about it; they simply use `git@…` URLs.

So there are two acquisition shapes and the operator picks per box: **mint** (nothing transits) or
**place/export** (unattended-capable).

### At rest: a stated non-goal

**smith does not protect secrets at rest on the box.** Written down as a non-goal, not left
implicit.

Encryption at rest needs a key; the key must live where the same passwordless-sudo user can read
it; the problem has moved one hop while acquiring the appearance of a solution.

- **`perms` (default `0600`) is an accidental-exposure guard**, explicitly a speed bump and not a
  boundary — the same honesty [ADR-0005](./0005-terminal-rides-the-ssh-door.md) applies to
  advisory read-only.
- **The real control is blast radius and disposability**: fine-grained PATs scoped to the
  blueprint's repos, per-repo deploy keys, keys cheap enough to rotate, boxes cheap enough to
  destroy. Scoped-and-disposable is *policy* that composes with the one path, not a competing
  mechanism.

### The schema hard-errors a literal

**A value in an `env:` map or a placement `from:` that does not parse as a known scheme is a
validation error, not a warning.** A blueprint is committed and shared, which makes it a far more
dangerous home for a literal than shell history ever was.

Known schemes are **`file:`, `env:`, `literal:`**. `literal:` exists because non-secret exported
variables are real — `NODE_ENV`, `LOG_LEVEL`, `ACME_REGION` — and forcing an operator to set a
shell variable in order to export a constant is absurd enough that the rule would be resented and
worked around:

```yaml
env:
  GITHUB_TOKEN: "env:GH_TOKEN"      # reference
  LOG_LEVEL:    "literal:debug"     # plain value, deliberately marked
```

The rule is *must parse as a **known** scheme*, not *must contain a colon* — otherwise
`LOG_FORMAT: "json:pretty"` parses as scheme `json` and fails with a baffling message. Unknown
scheme is its own named error: `unknown secret scheme "json" (known: file, env, literal)`.

**`literal:` is not valid in a placement `from:`.** A placement source resolves to a whole file's
bytes, so `literal:` there would mean inline file content — a heredoc feature, and precisely the
shape you least want holding a secret.

What this buys, stated honestly: **it prevents nothing.** Someone in a hurry types
`literal:ghp_abc` and commits a token. It buys two things — they had to do it *on purpose*, and
`grep -r 'literal:' ~/.smith/blueprints` finds every one in a second. "Is there a secret in this
blueprints directory?" becomes a mechanical check a pre-commit hook or CI can run, instead of a
judgement call over every string in every file. Under permissive parsing that question has no
mechanical answer at all.

### The provider token never touches a box — by construction

`machine create` structurally cannot run on a box that does not exist yet, and `destroy`/`list`
are *about* boxes, so the provider adapter is inherently operator-side
([ADR-0007](./0007-the-provider-adapter-is-data.md)). The single fattest-blast-radius credential
in the system — it can create and destroy machines — is therefore never at rest anywhere smith
put it. Recorded as a deliberate property so nobody later "improves" the adapter by staging its
token.

### smith is credential-agnostic

**smith places files and exports variables. It knows nothing about GitHub, Anthropic, or npm by
name.** The concrete case: a blueprint declaring `GH_TOKEN` with `https://` clone URLs will not
authenticate, because git needs a credential helper — and smith does **not** notice and fix that.
The moment it knows about GitHub it owes GitLab, `gh` version skew, and a choice between auth
modes, whereas "place this file, export this variable" covers every forge and every model provider
without changing. The first-run cost is real and is paid with a **worked example blueprint** that
gets it right.

The three box credentials and their slots:

| Credential | Slot |
|---|---|
| **git identity** (`user.name`, `user.email`) | **Not a secret.** Blueprint fields smith writes — a `once` placement of `~/.gitconfig` is heavier than needed |
| **forge auth** | A minted key, or a fine-grained PAT as `GH_TOKEN` in box-scoped `env:` |
| **coding-agent API key** | Box-scoped `env:` — every coding agent reads its key from the environment |

One collision this creates, resolved: git identity as blueprint fields means smith generates
`~/.gitconfig`, and an operator placement to the same path gives two writers on one path. Writing
git's XDG path instead is a trap, since git reads `~/.gitconfig` *or* `~/.config/git/config`,
never both. So **declaring git identity fields and a placement to `~/.gitconfig` is a validation
error** — mutually exclusive. The operator picks: smith manages it, or you do.

### Rotation and revocation

Rotation needs no new machinery — the two-level converge already provides the path:

```
rotate at source ──`machine setup`──▶ box stage ──`stop` then `start`──▶ worktree
```

A changed source reaches the stage on the next `machine setup`, and each worktree on the next
session `stop`/`start`, because connecting to a *live* session deliberately never swaps a file
under a running process.

**Revocation is the operator's, at the forge or provider.** smith's only contribution is making
*what does this box hold* answerable — which the blueprint already is, being the single
declaration site for every credential on the box.

## Considered options

- **SSH agent forwarding** — rejected, not deferred. A `tmux` session outliving operator
  disconnect is the whole point of the substrate, so a forwarded agent dies exactly when a long
  session needs to push — silently, at the worst moment, long after the operator stopped watching.
  It also breaks the driver-agnostic promise: forwarding is strong for HITL and useless for AFK,
  so adopting it would mean the downstream map inherits a new credential concept after all.
- **Encryption at rest, or a real permission boundary** — rejected for v1 and moved to its own
  future effort. Passwordless sudo makes every half-measure a speed bump wearing a boundary's
  clothes. A contributor is most likely to break this by adding exactly such a half-measure;
  stating the non-goal is what makes that reviewable.
- **A key-in-file scheme** (`dotenv:~/.secrets/api.env#GITHUB_TOKEN`) — deferred. It makes smith
  parse dotenv format, which is deceptively ambiguous (`export` prefixes, quoting, multiline
  values), and operators who keep secrets in files overwhelmingly already run something that puts
  them in the environment. Additive later via the same clean-room slot. See the consequence below
  for the gap it leaves.
- **Splitting `env` into a references map and a plain-values map** — rejected. It doubles the
  schema surface and asks the operator to classify every variable, which they will get wrong in
  the direction that hurts.
- **A secret-shape detector** — rejected. A heuristic wrong in both directions, whose false
  negatives teach people the validator has their back when it does not.
- **Letting `env` come from preferences** — rejected, even though box credentials pass the
  preferences test cleanly (your forge token genuinely does follow you to every box). The marker
  records **which blueprint** a box was built from and nothing about preferences; if
  `GITHUB_TOKEN` could come from preferences, a teammate cloning `~/.smith` and running
  `machine setup prod` gets a box with no token while both markers claim `blueprint: prod`. The
  pointer would be a lie about what the box contains. What is protected is *the same set of keys,
  never the same values* — `env:GH_TOKEN` already resolves differently on each laptop — and that
  is still worth protecting.

## Consequences

- **There is no way to name one value inside a file.** `env:GH_TOKEN` reads the operator's
  **shell** environment, not any file. An operator keeping secrets in a dotenv file who wants a
  single variable exported must first get it into their own shell (`direnv`, a `source`, a shell
  plugin). This bites hardest on box credentials, which are exactly the case needing a specific
  named value rather than a whole file.
- **A placed file's variables are not exported.** Needing both — the file on disk for a bundler,
  the variables exported for a CLI — costs two declarations in v1.
- **Box-scoped placements stay**, and are not an unexercised path: box credentials are frequently
  *files* — `~/.ssh/config` and a forge `known_hosts` entry, `~/.npmrc`, `~/.aws/credentials`, a
  git credential helper.
- **The staged placement bytes on the box are plaintext**, at `/etc/smith/placements/`, `0700`
  smith-owned ([ADR-0006](./0006-the-blueprint-config-surface.md)). Same non-goal, same reasoning;
  it is not a separate exposure.
- **Duplication across blueprints is accepted.** Repeating two or three credential lines per
  blueprint is the cost of the marker's promise, and blueprints are few. If it ever gets painful
  the fix is **blueprint inheritance**, not preferences.
- **A destroyed box takes its marker with it**, so knowing what a since-destroyed box held depends
  on remembering which blueprint built it. Recording the blueprint per box in the local inventory
  would give revocation-after-destroy for free, and
  [ADR-0011](./0011-the-box-inventory-is-identity-only.md) declined it — the inventory holds
  identity only, so this gap stays open for whoever ships box `destroy`. Nothing here depends on
  it.
- **Anyone adding a second acquisition path re-opens this decision.** A fetch-at-use-time
  integration, an on-box agent, or a smith that knows one forge by name each breaks either the
  driver-agnostic guarantee or the credential-agnostic one, and must be weighed against them
  rather than slipped in beside.

See [issue #67](https://github.com/byranZA/smith/issues/67).
