# Provisioning a box

`smith machine setup` takes a fresh box and makes it a secure, reachable
development machine: it creates a `smith` user with your SSH key, enables a
default-deny firewall, hardens SSH (no root login, no passwords), and turns on
fail2ban and automatic security updates. It then installs smith itself onto the
box and converges the workspace its blueprint declares.

Re-running is safe — every step checks before it changes anything.

## Prerequisites

You'll need the `smith` binary on your `PATH` — see [Installing
smith](./install.md).

The box you're provisioning must be:

- A fresh **Ubuntu 24.04 LTS or newer**.
- Reachable over SSH as **root**, or as a user with **passwordless sudo**. Smith
  connects non-interactively, so `ssh <login>@<host>` must log you in with **no
  prompt at all** — including no key passphrase (load a passphrase-protected key
  into `ssh-agent` first). This is the same access you used to reach the box;
  smith reuses it, copying that login's `authorized_keys` onto the new `smith`
  user.

If you'd rather smith created the box too, describe your provider's CLI as data
in a `provider` block and run `smith machine create dev --blueprint acme` — see
[providers](./providers.md).

## Provision over public SSH

Point smith at the box as `<login>@<host>`:

```sh
smith machine setup root@203.0.113.10 --name dev
# or a sudo-capable user:
smith machine setup ubuntu@203.0.113.10 --name dev
```

When it finishes, the box is reachable as the `smith` user over hardened public
SSH, and smith has written down the address it just proved — so you address the
box by the name you gave it from now on:

```sh
smith machine status dev
```

`status` reports how the box has drifted from what setup established, and never
changes anything. Its argument follows the rule every smith verb shares: **the
`@` decides**. A value containing `@` is an SSH target used exactly as written;
a value without one is looked up in the box inventory (`smith machine list`),
and on a miss is handed to `ssh` as written, so an `ssh_config` alias works too.
See [the box inventory](./inventory.md).

## Provision over Tailscale

To reach the box over a tailnet instead of the public internet, use
`--access tailscale`. This requires the machine you're running smith from to
already be a member of the tailnet (the `tailscale` CLI installed and running),
plus a Tailscale auth key passed as a reference — `env:VAR` or `file:/path`,
never a bare literal.

### First, add two entries to your tailnet ACL policy

An auth key can join a node but can't edit policy, so smith can't add these for
you:

```json
// declares tag:smith so the enrolled node's key never expires
"tagOwners": { "tag:smith": ["autogroup:admin"] },

// grants your identity SSH access to tag:smith (replace <your-identity>)
"ssh": [
  { "action": "accept", "src": ["<your-identity>"], "dst": ["tag:smith"], "users": ["smith"] }
]
```

Without the first, the box enrolls but never reaches `Running`; without the
second, it enrolls but the SSH probe is denied. (Prefer to run first? smith
prints these personalized to your identity and stops so you can add them.)

### Then provision

```sh
smith machine setup ubuntu@203.0.113.10 \
  --access tailscale \
  --tailscale-auth-key env:TS_AUTHKEY
```

Omit `--tailscale-auth-key` on an interactive terminal and smith prompts for it
without echoing. Smith only closes public SSH once it has verified the box is
reachable over the tailnet, so a policy that isn't ready yet never locks you
out.

A successful tailscale setup registers the **tailnet** address — the one the
lock-out-safety probe came in over — and tells you the name to use from then
on:

```
tailscale reach established over 100.92.14.7; public SSH closed.
registered smith@100.92.14.7 as "dev"

Reach it by name from now on:
  smith machine status dev
  smith machine setup dev
```

### Tailscale notes

- **Access is keyless after enrollment.** Once the box is on the tailnet you no
  longer manage an SSH key to reach it — Tailscale authenticates you by identity
  and the ACL rule decides access. The box's public SSH (port 22) is closed, so
  the tailnet is the only way in.
- **Authorization follows you, not one machine.** The ACL `src` is your tailnet
  user login, which matches every device you own on the tailnet. Any of your
  machines that's on the tailnet can `ssh smith@smith-<host>` or run
  `smith machine status smith@smith-<host>` — no key, no per-machine setup. To run
  `machine setup` from another machine it also needs the `tailscale` CLI and to
  be a running tailnet member.
- **The first provision still uses your SSH key.** A brand-new box isn't on the
  tailnet yet, so the initial `--access tailscale` run reaches it over public SSH
  as your bootstrap login (key-based) to install and enroll Tailscale, then
  closes public 22. The keyless model applies to everything after that.

The design reasoning is in
[ADR-0001](./adr/0001-tailscale-ssh-keyless-tagged-node.md) and
[ADR-0004](./adr/0004-lockout-gate-on-box-self-reverting-self-test.md).

## What runs after the bootstrap

`machine setup` ends with an ordered pipeline of named stages — config staging,
installing smith onto the box, and the workspace convergence. Those are covered
in [smith on the box](./on-box.md) and [the workspace
stage](./workspace.md).
