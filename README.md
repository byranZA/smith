# smith

Smith turns a fresh VPS into a ready-to-use remote development machine and
manages coding agents across the repositories on it. You bring the box; smith
provisions it, sets up the repos and toolchain you declared, and runs the
sessions your agents work in.

**Project status: early development.** Smith is currently being built for
personal use and experimentation. Interfaces, configuration and internal
architecture may change without notice, and there is no `v0.x` compatibility
promise — pin to an exact tag.

## How it works

Smith is one static binary that lives in two places. On your machine it is a
control surface. `machine setup` **installs the same binary onto the box**, and
the verbs that do work there — the session verbs and `workspace converge` — run
there; your local smith relays them over SSH and streams the box's output back.
That is what lets an agent keep working after you close your laptop.

Three things make a box a smith box:

- A **blueprint** — one YAML file declaring what kind of box this is: its repos,
  runtimes, placed files, and access mode.
- The **box inventory** — a local name → address book, so you type `dev` instead
  of an address that changed when the box was hardened.
- **Sessions** — one `tmux` session plus one git worktree of one repo on one
  branch, the unit of parallel work on a box.

## Quick start

Install (macOS Apple silicon shown — [other platforms and checksum
verification](docs/install.md)):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_arm64.tar.gz | tar xz
sudo mv smith /usr/local/bin/
smith version
```

> Fetch with `curl`, not your browser: smith is unsigned, and a browser's
> quarantine flag makes macOS refuse to run it. See
> [Gatekeeper](docs/install.md#gatekeeper-macos).

Provision a fresh Ubuntu 24.04 box you can already reach over SSH as root or a
passwordless-sudo user:

```sh
smith machine setup root@203.0.113.10 --name dev
```

Smith creates a `smith` user with your SSH key, enables a default-deny firewall,
hardens SSH, turns on fail2ban and unattended upgrades, installs smith on the
box, and writes down the address it just proved. From then on you use the name:

```sh
smith machine status dev                                # drift report; changes nothing
smith session start dev --repo smith --branch spec-42   # cut, place, launch, connect
```

To reach the box over a tailnet instead of the public internet, add
`--access tailscale` — that path needs two entries in your tailnet ACL policy
first. [Provisioning a box](docs/provisioning.md) covers both modes end to end.

## What smith does

**Provisioning.** `machine setup` hardens a fresh box and is safe to re-run —
every step checks before it changes anything. It works over public SSH or over
Tailscale, and only closes public SSH once it has proved the tailnet route
works, so a half-ready ACL never locks you out.
→ [docs/provisioning.md](docs/provisioning.md)

**Blueprints.** A blueprint (`~/.smith/blueprints/`) declares what kind of box
smith builds; preferences (`~/.smith/preferences.yaml`) hold what belongs to you
across every box. Both are optional — every field also has a flag and a default,
resolved **flag → blueprint → preference → default**. `smith blueprint check`
validates them offline and prints the resolved configuration with the source of
each value.
→ [docs/blueprints.md](docs/blueprints.md), a worked
[blueprint](docs/examples/blueprints/acme.yaml) and
[preferences](docs/examples/preferences.yaml)

**Making the box too.** Describe your provider's own CLI as data in a `provider`
block and `smith machine create dev --blueprint acme` creates the box before
provisioning it. Optional — without it you bring the box yourself.
→ [docs/providers.md](docs/providers.md)

**Boxes by name.** A successful setup registers the address it proved in
`~/.smith/cache/boxes.json`; `machine list`, `add` and `forget` manage it. In
every verb, **the `@` decides**: a value containing `@` is an SSH target used as
written, a value without one is looked up in the inventory and, on a miss,
handed to `ssh` — so an `ssh_config` alias or a MagicDNS name works with no
smith configuration at all.
→ [docs/inventory.md](docs/inventory.md)

**The workspace.** Setup's last stage converges the box to the workspace its
blueprint declares — placements, packages, the pinned toolchain, then a bare
clone of every declared repo. It is a verb of its own, so you can re-converge
without re-running the pipeline, and it never deletes: a step it cannot converge
is reported and what the box already holds stays put.
→ [docs/workspace.md](docs/workspace.md)

**Sessions.** Five verbs — `start`, `attach`, `list`, `stop`, `rm` — over
sessions named `<repo>-<branch>`. `start` is ensure-running, `attach` is
read-only unless you ask for `--interact`, and `list` is the work-state readout
you check before destroying a box by hand:

```
NAME             STATE    DIRTY  UNPUSHED
smith-main       live     -      0
smith-spec-42    stopped  dirty  3

2 sessions · 1 dirty · 1 with unpushed commits
```

Attaching adds no listener, no port, no tunnel and no new credential — it is
`tmux attach` through the SSH door setup already built.
→ [docs/sessions.md](docs/sessions.md)

**smith on the box.** Local and on-box smith must be the same version: every
relayed command declares the version it was built by, and on-box smith refuses a
mismatch rather than silently misparsing a renamed flag. `smith machine upgrade
dev` converges the box to *your* version — including backwards.
→ [docs/on-box.md](docs/on-box.md)

> **smith knows no service by name.** It places files and exports environment
> variables; it knows nothing about GitHub or npm. A blueprint declaring
> `GITHUB_TOKEN` beside `https://` clone URLs **will not authenticate** — git
> needs a credential helper for that, and smith will not notice. Clone over SSH
> and place the key, or place the helper's config yourself.

## Documentation

[docs/](docs/README.md) is the full index. The decision records behind the
behaviour above are in [docs/adr/](docs/adr/).

## Contributing

Smith is **not accepting outside pull requests yet** — it is early enough that
the design still moves faster than a review could keep up with. Bug reports and
questions are welcome as [issues](https://github.com/byranZA/smith/issues).

[CONTRIBUTING.md](CONTRIBUTING.md) has the detail, plus fresh-machine setup and
the quality gate for anyone working in the repository.

## License

[Apache 2.0](LICENSE).
