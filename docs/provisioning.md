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
  user. A key at a non-default path belongs in a `Host` entry in
  `~/.ssh/config`, and that alias works as a smith argument.
- Running sshd on **port 22**. The firewall smith enables allows SSH on 22
  only, so other ports aren't supported yet.

We recommend at least **2 GB of memory** to run an agent. A smaller box is still provisioned. See [Memory and swap](#memory-and-swap).

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

To reach the box over a tailnet instead of the public internet, use `--access tailscale`, or set `access: tailscale` in the blueprint or your preferences: setup resolves it like `blueprint check` does. This requires the machine you're running smith from to already be a member of the tailnet (the `tailscale` CLI installed and running), plus a Tailscale auth key passed as a reference — `env:VAR` or `file:/path`, never a bare literal.

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
  --name dev \
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

```text
tailscale reach established over 100.92.14.7; public SSH closed.
registered smith@100.92.14.7 as "dev"

Reach it by name from now on:
  smith machine status dev
  smith machine setup dev --blueprint <blueprint>
```

### Tailscale notes

- **Access is keyless after enrollment.** Once the box is on the tailnet you no
  longer manage an SSH key to reach it — Tailscale authenticates you by identity
  and the ACL rule decides access. The box's public SSH (port 22) is closed, so
  the tailnet is the only way in.
- **Authorization follows you, not one machine.** The ACL `src` is your tailnet
  user login, which matches every device you own on the tailnet. Any of your
  machines that's on the tailnet can `ssh smith@<tailnet IP>` or run
  `smith machine status smith@<tailnet IP>` — no key, no per-machine setup. The
  tailnet IP is the address in the setup report's `tailscale reach established
  over …` line, `100.92.14.7` above. To run `machine setup` from another
  machine it also needs the `tailscale` CLI and to be a running tailnet member.
- **The node name is requested, not guaranteed.** Enrollment asks Tailscale to
  name the node `smith-<host>`, made into one valid DNS label: lowercased, every
  character outside `a-z`, `0-9` and `-` replaced by `-`, runs of `-` collapsed,
  cut to 63 characters and trimmed of `-` at either end. Bootstrapping
  `203.0.113.10` requests `smith-203-0-113-10`, not `smith-203.0.113.10`. If
  another node on the tailnet already holds that name, Tailscale assigns a
  suffixed MagicDNS name instead, so check the admin console or `tailscale
  status` before reaching the box by name. The tailnet IP always works.
- **The first provision still uses your SSH key.** A brand-new box isn't on the
  tailnet yet, so the initial `--access tailscale` run reaches it over public SSH.
  The bootstrap phases, including installing the Tailscale package, run as your
  bootstrap login. The access stage then enrolls the box as the `smith` user,
  proves the route as `smith@<tailnet IP>`, and closes public 22 over the
  tailnet. The keyless model applies to everything after that.

The design reasoning is in
[ADR-0001](./adr/0001-tailscale-ssh-keyless-tagged-node.md) and
[ADR-0004](./adr/0004-lockout-gate-on-box-self-reverting-self-test.md).

## Memory and swap

An agent plus a typical build or test run needs well over 512 MB. On a box without swap, running out of memory freezes the box or gets the agent killed mid-task. Smith therefore recommends 2 GB of memory and gives a box without swap a swapfile.

### The memory advisory

Setup reads the box's memory before any phase runs and reports it with the preflight result. A box under the recommended 2 GB also gets a warning:

```text
preflight passed
memory: 458 MB
warning: under the recommended 2 GB of memory to run an agent
```

The advisory only warns. Setup still runs every phase and exits as it would on a larger box, because a bare hardened box is still a valid result. The advisory counts RAM, not swap. A nominal 2 GB box, whose kernel keeps some memory for itself and reports about 1.9 GB, gets no warning. If the box's memory can't be read, the report shows `memory: unknown` and gives no warning.

`smith machine status` notes the box's memory and swap under its verdict, with the same warning on a box under 2 GB:

```text
  memory: 1.0 GB
  warning: under the recommended 2 GB of memory to run an agent
  swap: 2.0 GB
```

The note never changes the verdict or the exit code. A small box whose facts all match its marker still reports matches.

### The swap phase

`swap` is the first base-layer phase, before `packages`, so the rest of setup already runs with swap. On a box with no active swap it creates `/swapfile`, readable only by root, enables it, and adds an `/etc/fstab` entry so it comes back after a reboot. The swapfile is the smaller of 2 GB and a quarter of the free disk on its filesystem: 8 GB free gives 2 GB of swap, 2 GB free gives 512 MB.

The phase adapts to the box rather than failing setup. In each of these cases it prints a note, is recorded as complete, and setup goes on to `packages`:

- **The box already has swap.** Any active swap, whether smith made it or not, is left exactly as it is and no swapfile is added (`swap already present`). On a re-run this is how the phase sees the swapfile it made earlier; if that swapfile has lost its `/etc/fstab` entry, the phase puts the entry back.
- **The disk is too small.** A swapfile under 256 MB isn't worth having, so with less than about 1 GB free the phase makes none (`swap skipped: not enough free disk`).
- **The box refuses swap.** Some virtualized boxes don't allow swap. When enabling it is refused, the phase removes the swapfile and its `/etc/fstab` entry (`swap skipped: not supported on this box`).

### Boxes set up before the swap phase

A box provisioned by an older smith has a marker that records every phase except `swap`, so `machine status` reports it as partially provisioned, naming `swap` as missing:

```text
✗ partially provisioned: setup did not complete
  memory: 3.8 GB
  swap: none

  missing phases: swap

  Re-run `smith machine setup <login>@<host>` to finish — the re-run resumes.
```

Re-run `smith machine setup <name>` to finish it. The re-run adds swap where the box allows it and records the phase, and status then reports the box as it would any other.

## What runs after the bootstrap

`machine setup` ends with an ordered pipeline of named stages:

1. `access` — in tailscale mode, enroll the box and close public SSH. In public
   mode it does nothing.
2. `install` — put smith onto the box.
3. `config` — stage the blueprint, when `--blueprint` is given.
4. `workspace` — converge it, relayed to the smith on the box.

Every stage reaches the box as the `smith` user, because hardening has closed
the bootstrap login by then. They are covered in [smith on the
box](./on-box.md) and [the workspace stage](./workspace.md).

### Without a blueprint

A setup that names no `--blueprint` still succeeds: the box is provisioned, secured, given smith and registered, and the run exits 0. It is a bare box, with nothing staged in `/etc/smith/` beyond the bootstrap marker and no `~/workspace`. The workspace stage says so rather than leaving you to find out from the marker:

```text
▶ workspace
no blueprint was staged, so the workspace had nothing to converge: no repos, tools, env or placements
✓ workspace
```

The closing re-run line then carries the flags the run went without. It adds `--blueprint <blueprint>` when none was staged, and `--name <name>` when the box is named after its host because nothing else named it:

```text
registered smith@203.0.113.10 as "203.0.113.10"

Reach it by name from now on:
  smith machine status 203.0.113.10
  smith machine setup 203.0.113.10 --blueprint <blueprint> --name <name>
```

A box set up with both `--blueprint` and `--name` ends with a plain `smith machine setup <name>`. A box that was built from a blueprint refuses a re-run without one, naming the blueprint to pass.

## Interrupting a run

Ctrl-C cancels the running command and lets it remove what it shipped to the
box. A second Ctrl-C, or 7 seconds without one, ends smith at once. An
interrupted run prints nothing more and exits 128 plus the signal number (130
for Ctrl-C).
