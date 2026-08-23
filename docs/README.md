# smith documentation

Start at the repository [README](../README.md) for what smith is and a
five-minute path from a fresh VPS to a running session. These pages carry the
detail.

## Getting a box running

| page | what it covers |
| --- | --- |
| [Installing smith](./install.md) | release downloads per platform, checksum verification, `go install`, building from source, Gatekeeper and SmartScreen, the `v0.x` compatibility position |
| [Provisioning a box](./provisioning.md) | `machine setup` prerequisites, provisioning over public SSH, provisioning over Tailscale including the two ACL entries you must add yourself |
| [Provider adapters](./providers.md) | describing your provider's CLI as data so `machine create` can make the box too |

## Using a box

| page | what it covers |
| --- | --- |
| [Blueprints and preferences](./blueprints.md) | the config home, the split between the two files, precedence, and what smith does and does not do about credentials |
| [The box inventory](./inventory.md) | the four `machine` verbs, naming rules, collisions and renames, the file's schema versioning |
| [The workspace stage](./workspace.md) | what `workspace converge` converges, the order the steps land in, and why it is that order |
| [Sessions](./sessions.md) | the five session verbs and their flags, the three paths through `start`, the two access levels, what `rm` destroys, the pre-teardown read |
| [smith on the box](./on-box.md) | the setup pipeline's stages, the relay, version skew and `machine upgrade`, the refusal paths |

## Examples

- [A full blueprint](./examples/blueprints/acme.yaml) and matching
  [preferences](./examples/preferences.yaml)
- Provider adapters: [doctl](./examples/adapters/doctl.yaml),
  [doctl with a name marker](./examples/adapters/doctl-name-marker.yaml),
  [hcloud](./examples/adapters/hcloud.yaml)

## Design decisions

[`docs/adr/`](./adr/) holds the architecture decision records — one file per
decision, each stating the context, the decision, and its consequences. They are
the *why* behind behaviour the pages above describe.

## Contributing

Outside pull requests are not being accepted yet; issues are welcome.
[CONTRIBUTING.md](../CONTRIBUTING.md) covers that, plus fresh-machine setup, the
quality gate, and how work is tracked.

## Notes for agents

[`docs/agents/`](./agents/) documents the workflows an agent working in this
repository follows — the [issue tracker](./agents/issue-tracker.md), [triage
labels](./agents/triage-labels.md), and the [domain docs](./agents/domain.md).
[`docs/research/`](./research/) holds primary-source research captured while
building a feature.
