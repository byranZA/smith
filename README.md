# smith

**Project status: early development.** Smith is currently being built for
personal use and experimentation. Interfaces, configuration and internal
architecture may change without notice.

Smith turns a fresh VPS into a ready-to-use remote development machine and
manages coding agents across the repositories on it.

## Provisioning a VPS

`smith machine setup` takes a fresh box and makes it a secure, reachable
development machine: it creates a `smith` user with your SSH key, enables a
default-deny firewall, hardens SSH (no root login, no passwords), and turns on
fail2ban and automatic security updates. Re-running is safe — every step checks
before it changes anything.

### Prerequisites

Build the binary:

```sh
make build   # produces ./bin/smith
```

The box you're provisioning must be:

- A fresh **Ubuntu 24.04 LTS or newer**.
- Reachable over SSH as **root**, or as a user with **passwordless sudo**. Smith
  connects non-interactively, so `ssh <login>@<host>` must log you in with **no
  prompt at all** — including no key passphrase (load a passphrase-protected key
  into `ssh-agent` first). This is the same access you used to reach the box;
  smith reuses it, copying that login's `authorized_keys` onto the new `smith`
  user.

### Provision (public)

Point smith at the box as `<login>@<host>`:

```sh
smith machine setup root@203.0.113.10
# or a sudo-capable user:
smith machine setup ubuntu@203.0.113.10
```

When it finishes, the box is reachable as the `smith` user over hardened public
SSH. Confirm it:

```sh
smith machine status 203.0.113.10
```

`status` connects as `smith` and reports how the box has drifted from what setup
established (it never changes anything — pass a bare `<host>`, not
`<login>@<host>`).

### Provision over Tailscale

To reach the box over a tailnet instead of the public internet, use
`--access tailscale`. This requires the machine you're running smith from to
already be a member of the tailnet (the `tailscale` CLI installed and running),
plus a Tailscale auth key passed as a reference — `env:VAR` or `file:/path`,
never a bare literal.

**First, add two entries to your tailnet ACL policy.** An auth key can join a
node but can't edit policy, so smith can't add these for you:

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

Then provision:

```sh
smith machine setup ubuntu@203.0.113.10 \
  --access tailscale \
  --tailscale-auth-key env:TS_AUTHKEY
```

Omit `--tailscale-auth-key` on an interactive terminal and smith prompts for it
without echoing. Smith only closes public SSH once it has verified the box is
reachable over the tailnet, so a policy that isn't ready yet never locks you
out.

After a successful tailscale setup, smith prints the tailnet name to use from
then on:

```sh
smith machine setup smith@smith-<host>   # re-run over the tailnet
smith machine status smith-<host>        # check status over the tailnet
```

#### Tailscale notes

- **Access is keyless after enrollment.** Once the box is on the tailnet you no
  longer manage an SSH key to reach it — Tailscale authenticates you by identity
  and the ACL rule decides access. The box's public SSH (port 22) is closed, so
  the tailnet is the only way in.
- **Authorization follows you, not one machine.** The ACL `src` is your tailnet
  user login, which matches every device you own on the tailnet. Any of your
  machines that's on the tailnet can `ssh smith@smith-<host>` or run
  `smith machine status smith-<host>` — no key, no per-machine setup. To run
  `machine setup` from another machine it also needs the `tailscale` CLI and to
  be a running tailnet member.
- **The first provision still uses your SSH key.** A brand-new box isn't on the
  tailnet yet, so the initial `--access tailscale` run reaches it over public SSH
  as your bootstrap login (key-based) to install and enroll Tailscale, then
  closes public 22. The keyless model applies to everything after that.
