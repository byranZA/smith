# Provider adapters

smith can create the box for you. It does that without knowing anything about
your provider: you describe your provider's own CLI as data, and smith renders
and runs it.

An adapter is the `provider` block of a blueprint or of
[`preferences.yaml`](./blueprints.md), and **it is optional**. Without one,
nothing changes — you create the box yourself, in whatever dashboard or CLI you
already use, and hand smith the target:

```sh
smith machine setup root@203.0.113.10
```

With one, that first step is a command too:

```sh
smith machine create dev --blueprint acme
```

## The shipped examples

| File | Provider | Status |
| --- | --- | --- |
| [`adapters/doctl.yaml`](./examples/adapters/doctl.yaml) | DigitalOcean | **Verified** against a real lifecycle run |
| [`adapters/doctl-name-marker.yaml`](./examples/adapters/doctl-name-marker.yaml) | DigitalOcean, token without tag permission | **Verified** |
| [`adapters/hcloud.yaml`](./examples/adapters/hcloud.yaml) | Hetzner Cloud | **UNVERIFIED — never executed** |

The two DigitalOcean adapters were driven end to end against a real account
with `doctl` 1.166.0 — create, address, `sshd` banner, list, resolve the
address back to the id, destroy — in about forty seconds. The responses that
run produced are fixtures in smith's test suite, and both files are parsed and
run by it on every build, so a documented adapter cannot drift out of the
schema.

**The `hcloud` adapter has never been run.** Every line of it is inferred from
Hetzner's published documentation, because nobody on this project has a Hetzner
account. It is shipped because its label-versus-tag asymmetry is the reason
`marker` has separate `arg` and `expect` fields — but the flag spellings, the
response envelopes and the label semantics are all assumptions. Verifying it
against a real `hcloud` is outstanding work. Start from the DigitalOcean file.

## What smith supplies, and nothing else

Four placeholders. Every other argument in a template is your own literal text,
passed to the command exactly as you wrote it.

| Placeholder | Value | Templates |
| --- | --- | --- |
| `{{name}}` | the box name you typed | `create` |
| `{{marker_arg}}` | `marker.arg` with `{{value}}` filled in | `create` |
| `{{ssh_key}}` | `ssh_key`, verbatim | `create` |
| `{{id}}` | the box id | `destroy` — never executed |

A `{{placeholder}}` outside that set is a validation error naming the four,
rather than a literal `{{sshkey}}` sent to your provider.

**An argument that renders empty drops itself and the flag before it.** A flat
argument list has no other way to say *omit this*: an unset `{{ssh_key}}` would
otherwise become `--ssh-keys ""`, which the CLI rejects. The rule is mechanical
and the same for every placeholder, which is what makes an adapter that stamps
nothing a configuration rather than a special case.

### There are no `size`, `region` or `image` fields

Deliberately, and they will not be added. DigitalOcean's `s-2vcpu-4gb` and
Hetzner's `cpx31` have nothing in common but position in an argument list, so a
`size` field in smith's schema would be exactly the provider knowledge this
design exists to remove. They are your literal text, in your template.

## Reading the provider's answer

Whatever JSON your CLI writes is normalized to one canonical record — the box
id and its public address — by two sets of paths.

`record` locates the box inside whatever envelope the provider wraps it in, per
template, because one provider can wrap its two verbs differently:

```yaml
record:
  create: "[*]"     # doctl answers with a bare array
  list:   "[*]"
```

`extract` then reads the fields, relative to that record:

```yaml
extract:
  id: "id"
  ip: "networks.v4[type=public].ip_address"
```

The path grammar is four things:

| Path | Selects |
| --- | --- |
| `id` | a key |
| `public_net.ipv4.ip` | dotted descent |
| `[*]` | every element of an array |
| `networks.v4[type=public].ip_address` | elements whose field equals a value |

**The predicate is load-bearing, not a convenience.** A DigitalOcean droplet's
`networks.v4` has no stable order — in one account the public address came
second on one droplet and first on another — so `[0]` hands smith a `10.x`
private address roughly half the time, and the box is then unreachable for a
reason that looks like anything but an extractor bug. There is no positional
index in the grammar for that reason.

A path that matches nothing is an error naming the path, never a silent zero
value. A `null` where a collection was expected is *not* an error: an unset
`tags` decodes as `null` rather than `[]`, and that is an answer.

## The marker

`marker` is how smith stamps a box it creates so an adapter can recognise it
again. It has three fields, because all three differ across providers:

```yaml
marker:
  arg:    "smith={{value}}"   # what create passes
  read:   "labels.smith"      # where it reads back from
  expect: "{{value}}"         # the form it reads back in
```

`{{value}}` is the box name you typed. In `arg` it renders and the result
substitutes for `{{marker_arg}}` in the `create` template, so `smith machine
create dev` passes `--label smith=dev`.

Whether the marker is a label, a tag, or the box's own name is your adapter's
business — smith requires no native tag support, because a provider token can
be perfectly valid and still refuse to create tags:

| provider | storage | `arg` | `read` | `expect` |
|---|---|---|---|---|
| doctl | `tags[]` opaque strings | `smith:{{value}}` | `tags[*]` | `smith:{{value}}` |
| doctl, no tag permission | the box's own name | *(empty)* | `name` | `{{value}}` |
| hcloud *(unverified)* | `labels{}` key/value map | `smith={{value}}` | `labels.smith` | `{{value}}` |

The second row needs no special handling: an empty `arg` renders empty, and an
argument that renders empty drops itself and the flag before it, so `--tag-name`
never reaches the provider with nothing behind it. The two DigitalOcean rows are
the same file with a different `marker` block, and smith runs them down the same
path with no branch between them.

**`read` and `expect` are written now and read later.** smith writes a marker at
create and, in this version, **never reads one back** — teardown and
provider-side box discovery are the readers, and neither has shipped. Both
fields are accepted and validated so an adapter you write today stays correct
when they do.

**Consequence: two smith boxes cannot share a name at one provider.** The marker
is the box name, and its only job is answering "did smith create this box".
smith does not enforce this — nothing checks the provider for a name collision
before creating a box.

## Provider credentials

`requires` lists the **names** of environment variables the provider CLI needs.
Names only — no values, and no references to a store either:

```yaml
provider:
  requires: [DIGITALOCEAN_ACCESS_TOKEN]
```

smith checks each name is present in your environment **before it runs any
provider command**, then launches the command with your environment inherited,
so the CLI reads the token itself. smith never reads, resolves, stages or logs
the value, and no smith output can contain it.

**`requires` catches a missing credential and never an insufficient one.** A
token can be present, correctly named, and still lack the scope your template
needs — a DigitalOcean token without tag permission passes this check and is
refused only at create, with `403 ... missing the required permission
tag:create`. Nothing smith can check before running the command tells those two
apart, which is why the second worked adapter exists.

**It is optional.** An `hcloud` context keeps its token in
`~/.config/hcloud/cli.toml` with no environment variable at all, so an adapter
declaring no `requires` is checked for nothing and trusts the CLI's own
credential story.

## The ssh key reference

`ssh_key` names a key already registered at your provider, and it reaches the
command verbatim. smith does not interpret, resolve or validate it: a key name,
a fingerprint and a full public key line are all equally acceptable, because
smith cannot tell which your provider wants.

Nothing can pre-flight it either. A reference naming nothing, a key registered
under another name, and a token without permission to read keys are
indistinguishable to smith and identical to you: the box boots, port 22 answers,
and the first `ssh` returns `Permission denied (publickey)`. So `create` prints
the reference it passed — or states that it passed none — and names it as the
first thing to check. Declaring no `ssh_key` is legal and silent.

## What `create` does, and where it stops

```
$ smith machine create dev --blueprint acme
box created: 593069736
address:     203.0.113.10
ssh key:     "acme-box"
reachable:   port 22 is answering

nothing is provisioned yet. Run:
  smith machine setup root@203.0.113.10
```

In order: the `requires` pre-flight, the rendered `create` command, the record
extracted from its output, then — only if the provider reported no address yet
— the `list` command polled until the entry whose id matches has one, and
finally port 22 dialled until `sshd` answers, because no provider CLI waits for
that.

**`create` does not provision the box.** It writes nothing to it and runs no
bootstrap; it stops at a reachable box and hands you the `machine setup` line.
That is deliberate: a chained setup that failed halfway would leave you holding
a box that exists, is being billed, and that smith cannot tear down. Two
commands, and every failure leaves one clear next step.

Every way this gives up names what you now own — the box id, and the address if
it got one — for the same reason.

## `destroy` is specified and never executed

The contract carries a third template and the schema accepts and validates it,
so adding the verb later is cheap and an adapter written today stays correct.
**smith exposes no command that runs it.** Shipping it needs a confirmation
poll — `list` immediately after a *successful* `delete` still returned the box —
and box-teardown safety settled first. Until then, a box smith created is torn
down at your provider, by you.
