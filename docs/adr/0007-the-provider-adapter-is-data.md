# The provider adapter is data — and v1 creates boxes but never destroys one

## Context

Until now the operator brought the box: they clicked through a provider dashboard, got an IP, and
handed smith `<login>@<host>`. Standing up a box without ever opening that dashboard means smith
has to talk to a VPS provider — and the obvious way to do that, a Go SDK per provider, puts every
provider's API surface, auth model, and release cadence inside smith's binary for as long as it
lives.

[ADR-0002](./0002-secrets-are-references-not-values.md) already established the shape of the
alternative for secrets: config carries a *reference*, never the thing itself. The question was
whether the same move works for a whole provider — whether an operator-supplied **command
reference** can carry enough of a provider's behaviour that smith needs no knowledge of its own.

Three tickets answered it: a survey of what the provider CLIs actually expose
([#54](https://github.com/byranZA/smith/issues/54)), a full create→list→destroy lifecycle driven
against real `doctl` ([#60](https://github.com/byranZA/smith/issues/60)), and the last open
question in the create contract — whether registering an SSH key needs a fourth verb
([#72](https://github.com/byranZA/smith/issues/72)).

## Decision

**An adapter is data, not code: command templates plus field extractors, all emitting JSON on
stdout, normalized to a canonical record `{id, ip|null, marker}`.** smith holds no provider
knowledge whatsoever. The adapter lives in the `provider` block of a blueprint or of preferences
([ADR-0006](./0006-the-blueprint-config-surface.md)), and it is optional — absent one, the
bring-your-own-box path is unchanged.

**v1 ships `create` and `list`. Box `destroy` is a stated non-goal.** The contract specifies the
third verb and the marker records the provider destroy reference, so adding it later is cheap —
but smith never invokes it in v1, and the operator tears a box down at the provider by hand.

The prototype's load-bearing result is what makes "data, not code" more than an aspiration: when
the test token turned out to lack tag permission, swapping the entire reconciliation strategy from
tags to a name convention required **no change to the harness at all** — only a different adapter
file.

### smith must not require native provider tags

DigitalOcean returns `403 … missing the required permission tag:create` for `--tag-name` under a
droplet-scoped token. So the field is a **`marker`**, not a `tag`: the adapter declares *how to
read* it and *what value to expect*, and whether that is a tag, a label, or the box's own name is
the adapter's business.

It needs **two fields**, because what `create` passes and what the extractor reads back are
different strings across providers:

| provider | storage | create arg | read path | reads back |
|---|---|---|---|---|
| doctl | `tags[]`, opaque strings | `smith:<runid>` | `tags[*]` | `smith:<runid>` |
| hcloud | `labels{}`, key/value map | `smith=<runid>` | `labels.smith` | `<runid>` |
| doctl, no tag perm | the name itself | — | `name` | `smith-proto-<runid>` |

### Extractors take predicates, not positions

`networks.v4` ordering is **not stable** — the public IP came second on one droplet and first on
another in the same account. `networks.v4[type=public].ip_address` is load-bearing; a positional
path silently hands smith a `10.x` private address, and the box is then unreachable for a reason
that looks like anything but an extractor bug.

### Argv templates need optional-argument semantics

A flat argument list cannot express *omit this*. An unset `{{ssh_key}}` becomes `--ssh-keys ""`
and the CLI rejects it, so an unresolved placeholder drops the argument **and the flag in front of
it**.

### The SSH-key reference is an opaque literal, and there is no fourth verb

`provider.ssh_key`, placeholder `{{ssh_key}}`, passed straight into `create`. Hetzner and
DigitalOcean want the *name or id* of a key already registered on the account; Linode wants the
*raw public key text*. One opaque pass-through field serves both, and **absence is legal** — the
argument drops and smith says nothing about it.

Two facts in shipped code settled the one-key-or-two fork before the question was asked: smith
passes **no `-i`** when it connects (`internal/connection/connection.go:73` — authentication is
entirely the operator's local agent/config), and `phase_smith_keys` copies *the bootstrap login's
own* `authorized_keys` onto the `smith` user (`internal/bootstrap/bootstrap.sh:369`). So the
provider-create key **is** the box's key, transitively, and always has been. **One key,
operator-owned.**

### Credentials are ambient, and the provider token never reaches a box

`provider.requires` lists environment variable **names** — optional, checked pre-flight so a
missing token fails with smith's message rather than a raw provider error. smith inherits the
operator's environment into the subprocess and never reads, resolves, stages, or logs the values.
`requires` is optional because `hcloud` contexts hold the token in `~/.config/hcloud/cli.toml`
with no env var at all; absent, smith checks nothing and trusts the CLI's own credential story.

Provider commands are structurally **operator-side** — `machine create` cannot run on a box that
does not exist yet — so the single fattest-blast-radius credential in the system is never at rest
anywhere smith put it ([ADR-0009](./0009-provisioned-secrets-sit-in-plaintext.md)).

### The `provider` block replaces wholesale

This is **the single exception** to ADR-0006's field-level precedence. Preferences hold an
operator's default adapter *entire* — `create`, `list`, `destroy`, `requires`, `marker`,
`extract`, `ssh_key` — so under field-level merge, a blueprint declaring only a DigitalOcean
`create` template would silently inherit Hetzner's `requires`, `marker`, `extract` and `ssh_key`.
Every one of those is wrong, and all of them surface only at `create`. An adapter is a coherent
description of one provider's CLI; half of one and half of another is never a thing anyone wants.

This is also what makes `ssh_key` safe in *both* files: it can never outlive the templates that
give it meaning.

## Considered options

- **A Go SDK per provider** — rejected. It puts every provider's API surface inside smith's
  binary permanently, and the prototype showed the command-reference path costs nothing in
  capability: a real create→IP→list→resolve-id→destroy lifecycle ran against `doctl` 1.166.0 in
  ~40 seconds for ~$0.00007, with zero provider knowledge in the harness.
- **A fourth `ensure-key` verb** — rejected, by *deleting* the idea rather than specifying it. It
  buys unattended first-run on a fresh account at the cost of another command template in every
  adapter *and* another token scope — and scope failures are invisible until the call is made.
  The `403 tag:create` refusal and the missing `ssh_key` scope that raised the question both
  passed every pre-flight smith can run. Registering a public key is genuinely once-per-account
  and is the same shape as the provider token: ambient, operator-side.
- **Accepting `file:~/.ssh/id_ed25519.pub` as the key reference** — rejected, and this is the half
  worth defending. A bare Hetzner key *name* must stay legal, so the known-scheme hard error
  ([ADR-0009](./0009-provisioned-secrets-sit-in-plaintext.md)) cannot apply here — which would
  make this the one field in the schema where "known scheme **or** bare literal" is valid. That
  is the literal-vs-scheme ambiguity itself, not a smaller version of it. The cost is cosmetic: a
  public key is not a secret, and a blueprint is the committed-and-shared half of `~/.smith/` —
  exactly where a pubkey belongs.
- **A `provider.env` map of resolvable references** — rejected. More consistent with the rest of
  the schema, but it puts the provider token into smith's data path, and the moment smith holds
  that value, "does it get staged?" becomes a live question with a bad answer available.
- **Shipping `destroy` in v1** — ruled out of scope. `create` is the verb that pays for itself on
  a fresh VPS; `destroy` drags in enumerate-every-worktree teardown safety and full provider
  reconciliation. See the consequences below.
- **Fly.io as a v1 target** — excluded. Machines have no OS port 22; SSH is cert-based over
  WireGuard and needs its own adapter shape entirely.

## Consequences

- **Readiness is always smith's own poll.** No provider CLI waits for `sshd`. `create` is
  declared IP-synchronous or IP-deferred; when the IP comes back null, smith falls through to a
  `list`-by-id poll, then polls port 22 itself. "id always + IP maybe + poll-`list`-by-id" is
  first-class, not an AWS special case.
- **`destroy` is asynchronous, and a zero exit is not proof of teardown.** `list` immediately
  after a *successful* `delete` still returned the box. Whoever ships `destroy` needs a
  confirmation poll, exactly like the port-22 readiness poll smith already owns — and must settle
  box-teardown safety first. [ADR-0010](./0010-session-teardown-safety-and-the-work-state-readout.md)
  does **not** cover it: that is the single-worktree `session rm` case.
- **The marker/`marker` machinery is no longer load-bearing for teardown.** It survives in v1 only
  as the cache-reconstruction path. The marker still *records* the provider destroy reference —
  recording costs nothing, keeps manual teardown possible, and makes adding the verb later cheap.
- **No pre-flight can catch a bad key reference, a wrong key name, or an insufficient token
  scope.** All three surface identically: the box boots, the port-22 poll succeeds, and the first
  `ssh` returns `Permission denied (publickey)`. Prevention is off the table, so **the error
  message is the design** — `create` prints the key reference it passed (or explicitly that it
  passed none), and the connect-refused path names that reference as the first suspect.
  `provider.requires` guards against a *missing* credential, never an *insufficient* one, and
  nothing in the docs or the CLI should imply otherwise.
- **The marker records nothing about the SSH key.** The box's actual `authorized_keys` is ground
  truth, and the create-time reference has no meaning once the box exists — recording it gives the
  marker something it can be wrong about, for no verb's benefit. This deliberately *differs* from
  the provider destroy reference, which names a thing only the provider knows and nothing on the
  box can reconstruct.
- **`machine keygen`** ([ADR-0009](./0009-provisioned-secrets-sit-in-plaintext.md)) is unrelated
  and must not be reached for here: it mints on a box that already exists, and this key's entire
  job is to make the *first* connection to a box that does not.
- **Provider ids are JSON integers** and must not round-trip through a float; doctl's `tags`
  decodes as `null`, not `[]`, when unset.
- **`adapters/hcloud.json` from the spike is unverified** — written from vendor docs, no `hcloud`
  executed. Its label/tag asymmetry is the reason the two-field marker exists, so it is the
  highest-value thing to confirm before shipping.

See [issue #54](https://github.com/byranZA/smith/issues/54),
[issue #60](https://github.com/byranZA/smith/issues/60) and
[issue #72](https://github.com/byranZA/smith/issues/72). Spike on branch
[`prototype/provider-adapter`](https://github.com/byranZA/smith/tree/prototype/provider-adapter).
