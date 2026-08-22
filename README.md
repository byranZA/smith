# smith

**Project status: early development.** Smith is currently being built for
personal use and experimentation. Interfaces, configuration and internal
architecture may change without notice.

Smith turns a fresh VPS into a ready-to-use remote development machine and
manages coding agents across the repositories on it.

## Install

`smith` ships as a single static binary. Grab the [latest
release](https://github.com/byranZA/smith/releases/latest) with the one-liner
for your platform — it fetches the `v0.1.0` archive, unpacks it in the current
directory, and leaves the `smith` binary alongside its `LICENSE` and `README`.
Move it onto your `PATH` afterwards (e.g. `sudo mv smith /usr/local/bin/`).

> **Fetch with `curl`, not your browser.** smith is unsigned. A browser stamps
> every download with a quarantine flag, so macOS Gatekeeper then refuses to run
> it; `curl` and `wget` don't set that flag, so a curled binary runs untouched.
> The commands below are the supported path for exactly this reason. If you did
> download through a browser, see [Gatekeeper](#gatekeeper-macos) below.

**macOS** (Apple silicon):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_arm64.tar.gz | tar xz
```

**macOS** (Intel):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_amd64.tar.gz | tar xz
```

**Linux** (x86-64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_linux_amd64.tar.gz | tar xz
```

**Linux** (ARM64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_linux_arm64.tar.gz | tar xz
```

**Windows** (x86-64) — download the zip, then extract it (modern `tar` on
Windows 10+ handles zips):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_windows_amd64.zip
tar -xf smith_0.1.0_windows_amd64.zip
```

Confirm it: `smith version` should print `0.1.0`.

### Verify the checksum

The `curl | tar` one-liners are the fast path. To verify first, download the
archive to disk instead of piping it, check it against `checksums.txt`, then
unpack — shown here for macOS Apple silicon (substitute your archive name):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_arm64.tar.gz
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/checksums.txt

shasum -a 256 -c checksums.txt --ignore-missing   # macOS
sha256sum   -c checksums.txt --ignore-missing      # Linux

tar xzf smith_0.1.0_darwin_arm64.tar.gz
```

`--ignore-missing` verifies just the archive you downloaded and skips the rest.
Expect a single `... : OK` line.

### Install with `go install`

If you have the Go toolchain (1.26+, matching smith's `go.mod`), build and
install from the module tag directly:

```sh
go install github.com/byranZA/smith/cmd/smith@v0.1.0
```

This resolves the tag, builds from source, and stamps the version from the
module path — `smith version` still reports `0.1.0`, with no ldflags involved.
The binary lands in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`); make sure
that's on your `PATH`.

### Build from source

To build from a checkout instead:

```sh
make build   # produces ./bin/smith
```

A binary built this way reports `smith version` as `dev` — it carries no release
tag. Use `curl` or `go install` for a version-stamped build.

### Gatekeeper (macOS)

If you downloaded through a browser and macOS refuses to open smith — *"smith
cannot be opened because the developer cannot be verified"* — clear the
quarantine flag the browser set, then run it:

```sh
xattr -d com.apple.quarantine ./smith
```

Or just re-fetch with the `curl` one-liner above, which never sets the flag in
the first place.

### Windows / SmartScreen

The Windows story is weaker than the macOS one. SmartScreen keys off
*reputation* as well as Mark-of-the-Web, so a freshly published unsigned binary
can be flagged even when fetched with `curl`, and reputation only builds with
downloads over time. If SmartScreen blocks it, choose **More info → Run anyway**.

### Compatibility

smith is **`v0.x` — there is no compatibility promise.** Flags, config, and
behavior may change between releases without notice. Pin to an exact tag rather
than tracking `latest`.

## Provisioning a VPS

`smith machine setup` takes a fresh box and makes it a secure, reachable
development machine: it creates a `smith` user with your SSH key, enables a
default-deny firewall, hardens SSH (no root login, no passwords), and turns on
fail2ban and automatic security updates. Re-running is safe — every step checks
before it changes anything.

### Prerequisites

You'll need the `smith` binary on your `PATH` — see [Install](#install) above.

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
See [Boxes by name](#boxes-by-name) below.

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

#### Tailscale notes

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

## Boxes by name

Smith keeps a **box inventory** at `~/.smith/cache/boxes.json` — a name → address
book, and nothing else. A successful `machine setup` writes the address it just
proved into it; every verb reads it, so you type `dev` instead of an IP that
changed when the box was hardened.

```sh
smith machine list                            # instant, offline: names and targets
smith machine list --probe                    # ... and whether each one answers
smith machine add smith@100.92.14.7           # register a box smith already provisioned
smith machine add smith@100.92.14.7 --name api
smith machine forget dev                      # drop the entry; the box keeps running
```

`setup` takes `--name` to choose the name (its marker's name, the blueprint's
name, then the host are the fallbacks) and `--target` to register an address of
your own instead of the one smith proved.

**The `@` decides**, in every verb: a value containing `@` is an SSH target used
exactly as written and never looked up; a value without one is looked up in the
inventory and, on a miss, handed to `ssh` as written. That last part is free
interoperability — an `ssh_config` alias or a MagicDNS name works as a smith
argument with no smith configuration at all.

**Rebuildable, not self-rebuilding.** Nothing in `boxes.json` exists only there
— every fact about a box is on the box's marker — but smith cannot fetch it back
for you. Rebuilding an entry means SSHing to a box, which means already knowing
its address, and provider-side discovery is not in v1. A deleted `boxes.json` is
rebuilt **by hand, one `smith machine add` per box**, from your provider
dashboard or shell history; each box returns under the name its marker records.

[docs/inventory.md](docs/inventory.md) covers the four verbs, the naming rules,
collisions and renames, and the file's schema versioning in full.

## Blueprints

A **blueprint** declares what kind of box smith builds — its repos, runtimes,
placed files, access mode — as one YAML file in `~/.smith/blueprints/`.
**Preferences** (`~/.smith/preferences.yaml`) hold what belongs to you across
every box. Both are optional, and every field they carry can also come from a
flag or a built-in default: **flag → blueprint → preference → default**.

Validate one before you build anything — `check` writes nothing, touches no box,
and needs no network:

```sh
smith blueprint check              # validate your preferences
smith blueprint check acme         # validate preferences + the blueprint `acme`
smith blueprint check ./team.yaml  # validate a blueprint kept outside ~/.smith
```

It prints what smith would actually use, and where each value came from:

```
blueprint "acme" is valid (/home/ada/.smith/blueprints/acme.yaml)

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
```

Then the rest of what the blueprint declared — each repo's tools, env and
placements, followed by the box-wide `packages`, `tools`, `env` and
`placements`. Repos appear under the name they resolve to, so a name defaulted
from a clone URL is visible before any box exists.

Start from the worked examples — a full
[blueprint](docs/examples/blueprints/acme.yaml) and matching
[preferences](docs/examples/preferences.yaml) — and read
[docs/blueprints.md](docs/blueprints.md) for the config home, the split between
the two files, precedence, and what smith does and does not do about
credentials.

If you want smith to create the box too, describe your provider's own CLI as
data in a `provider` block and run `smith machine create dev --blueprint acme`.
It is optional — without it you bring the box as before —
and [docs/providers.md](docs/providers.md) covers it, alongside worked
[adapters](docs/examples/adapters/) for DigitalOcean.

> **smith knows no service by name.** It places files and exports environment
> variables; it knows nothing about GitHub or npm. A blueprint declaring
> `GITHUB_TOKEN` beside `https://` clone URLs **will not authenticate** — git
> needs a credential helper for that, and smith will not notice. Clone over SSH
> and place the key, or place the helper's config yourself.
