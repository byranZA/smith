# Tailscale SSH enrollment for smith's `--access=tailscale` VPS bootstrap

Research for GitHub issue #3. Verified against Tailscale's official KB / docs and the
`tailscale` source of record (`ipn/ipnstate`) as of 2026. Every non-obvious claim
cites the specific source it came from.

## Context / goal

For `--access=tailscale`, smith:

1. SSHes into a fresh Ubuntu LTS VPS over **public** SSH (password or a key smith placed).
2. Installs Tailscale and enrolls the box onto **the user's** tailnet with an auth key.
3. Enables Tailscale SSH on the box.
4. From the machine running smith (itself on the tailnet), verifies the box is online
   **and reachable over the tailnet**.
5. Only then closes the public SSH port.

The design question is the automate-vs-preconfigure boundary: what smith can do with only
an auth key vs. what the user must set up in their tailnet admin first.

---

## 1. Auth keys — minting a pre-authorized, tagged key for a persistent server

### Key-type decision (for a long-lived VPS)

| Property | Choice for smith | Why |
|---|---|---|
| **Pre-authorized** | **Yes** | Auto-authorizes the device even when the tailnet has device approval turned on, so no human has to click "approve" mid-bootstrap. A pre-approved key "automatically authorize[s] devices when device approval is enabled, bypassing manual authorization." ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys)) |
| **Tagged** | **Yes** — e.g. `tag:smith` | A tagged auth key "automatically assign[s] tags to provisioned devices, applying access control policies based on those tags." Tags are also what the SSH ACL will target as the destination. ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys)) |
| **Ephemeral** | **No** | Ephemeral means "automatically remove the auth key after the device goes offline" — for short-lived containers/Lambdas, **not** a persistent server. The VPS is long-lived, so it must be non-ephemeral. ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys)) |
| **Reusable vs. one-off** | **One-off (single-use)** per enrollment | One-off keys are "single-use... ideal for cloud servers where browser authentication isn't practical." Reusable keys authenticate many devices but the docs warn: "Be very careful with reusable keys! These can be very dangerous if stolen." Since smith enrolls **one** box per key, single-use is the safer default. Mint a fresh key per VPS. ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys)) |

**Bonus for persistence:** because the box is **tagged**, its node key does not expire on
the normal 180-day schedule — tagged devices have key expiry disabled by default, which is
exactly what a long-lived server wants. Note the auth key's own 1–90 day expiry (default 90)
only limits *when the key can be used to enroll*; it does **not** deauthorize an
already-enrolled device. ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys),
[tags KB](https://tailscale.com/kb/1068/tags))

### How to mint the key (two paths)

**Path A — admin console (human, one-time):** Owner/Admin/IT/Network admin goes to the
**Keys** page, "Generate auth key," and ticks *Pre-approved* + *Tags: `tag:smith`*, leaves
*Ephemeral* off, *Reusable* off. Registers with `tailscale up --auth-key=tskey-...`.
([auth-keys KB](https://tailscale.com/kb/1085/auth-keys))

**Path B — programmatic (recommended for smith automation):** Use an **OAuth client** with
the `auth_keys` scope. "When you create an OAuth client with the scope `auth_keys`, you must
select one or more tags" — so every OAuth-minted key is inherently tagged.
([OAuth clients KB](https://tailscale.com/kb/1215/oauth-clients))

Two sub-options:

- **`get-authkey` utility** (defaults to pre-authorized):
  ```shell
  # env: TS_API_CLIENT_ID / TS_API_CLIENT_SECRET
  go run tailscale.com/cmd/get-authkey@latest -tags tag:smith -preauth
  ```
  Flags: `-tags` (required), `-preauth` (default true), `-reusable`, `-ephemeral`.
  ([OAuth clients KB](https://tailscale.com/kb/1215/oauth-clients))

- **Direct REST call** `POST https://api.tailscale.com/api/v2/tailnet/-/keys`:
  ```json
  {
    "capabilities": {
      "devices": {
        "create": {
          "reusable": false,
          "ephemeral": false,
          "preauthorized": true,
          "tags": ["tag:smith"]
        }
      }
    },
    "expirySeconds": 3600,
    "description": "smith VPS bootstrap"
  }
  ```
  ([Tailscale API](https://tailscale.com/kb/1101/api),
  [OAuth clients KB](https://tailscale.com/kb/1215/oauth-clients))
  *(Body shape is the documented `capabilities.devices.create` schema; treat exact field
  names as API-versioned and validate against the live API reference.)*

### `tailscale up` flags smith runs on the box

```shell
tailscale up \
  --auth-key=tskey-xxxxx \        # enroll non-interactively
  --hostname=smith-<name> \       # set the machine name (drives MagicDNS name)
  --advertise-tags=tag:smith \    # apply tag (must match the key's tag / be tag-owned)
  --ssh                           # run the Tailscale SSH server
```

Flag semantics (all from the [CLI reference](https://tailscale.com/kb/1080/cli)):

- `--auth-key=<key>` — "Provide an auth key to automatically authenticate the node."
- `--hostname=<name>` — "Provide a hostname to use for the device instead of the one
  provided by the OS." This becomes the MagicDNS label.
- `--advertise-tags=<tags>` — "Give tagged permissions to this device. You must be listed in
  TagOwners to apply tags." (When the key is already tagged, the tag is applied by the key;
  passing it explicitly is fine and consistent.)
- `--ssh` — "Run a Tailscale SSH server, permitting access per the tailnet administrator's
  declared access policy."

### Ubuntu install (official apt repo, Noble 24.04 LTS)

```shell
sudo mkdir -p --mode=0755 /usr/share/keyrings
curl -fsSL https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg \
  | sudo tee /usr/share/keyrings/tailscale-archive-keyring.gpg >/dev/null
curl -fsSL https://pkgs.tailscale.com/stable/ubuntu/noble.tailscale-keyring.list \
  | sudo tee /etc/apt/sources.list.d/tailscale.list
sudo apt-get update && sudo apt-get install -y tailscale
```

The resulting repo line is
`deb [signed-by=/usr/share/keyrings/tailscale-archive-keyring.gpg] https://pkgs.tailscale.com/stable/ubuntu noble main`.
Swap `noble` for the codename of the target LTS. A one-line alternative exists
(`curl -fsSL https://tailscale.com/install.sh | sh`) but the pinned apt steps are more
auditable for smith's deterministic bootstrap.
([Install Tailscale on Linux](https://tailscale.com/docs/install/linux),
[stable packages](https://pkgs.tailscale.com/stable/ubuntu/noble.tailscale-keyring.list))

---

## 2. Tailscale SSH — does it remove `authorized_keys`, and what ACL is required?

### Enabling it

`tailscale up --ssh` (during enrollment) or `tailscale set --ssh` (on an already-up node)
starts the Tailscale SSH server. It "generates a host key pair and configures the daemon to
intercept port 22 traffic from your tailnet." ([Tailscale SSH KB](https://tailscale.com/kb/1193/tailscale-ssh))

### Does it replace `authorized_keys` / OpenSSH? — Effectively yes for tailnet SSH, but it does not touch OpenSSH

For connections **over the tailnet**, authentication is handled by Tailscale identity +
the policy file, so **no `~/.ssh/authorized_keys` entry is needed** for those sessions.
Critically, it does **not** modify OpenSSH: "Your SSH configuration
(`/etc/ssh/sshd_config`) and keys (`~/.ssh/authorized_keys`) files will not be modified,
which means that other SSH connections to the same host, not made over Tailscale, will still
work." Tailscale only intercepts traffic to the **Tailscale IP** on port 22.
([Tailscale SSH KB](https://tailscale.com/kb/1193/tailscale-ssh))

Implication for smith: once Tailscale SSH is up and the box is reachable over the tailnet,
smith no longer needs public-SSH key material — it can close the public port and rely on
Tailscale-identity SSH. The OS user still has to exist (e.g. `ubuntu`, `root`), but no
`authorized_keys` line is required for the tailnet path.

### Required ACL `ssh` rule

Tailscale SSH is **deny-by-default**: an `ssh` rule in the tailnet policy file must grant it.
Rule shape ([Tailscale SSH KB](https://tailscale.com/kb/1193/tailscale-ssh)):

```json
{
  "action": "accept",              // or "check" (see below)
  "src": ["autogroup:member"],     // who may SSH; users/groups/tags — never bare "*"
  "dst": ["tag:smith"],            // the destination device(s); port 22 is implicit
  "users": ["ubuntu", "root"],     // OS accounts allowed; or "autogroup:nonroot"
  "checkPeriod": "12h"             // only meaningful for action "check"
}
```

- `action: "accept"` — allows the authenticated tailnet user straight through.
- `action: "check"` — same, but forces periodic re-authentication (browser check) every
  `checkPeriod` (min 1m, max 168h, default 12h). If both an `accept` and a `check` rule match
  a connection, the more restrictive **`check`** wins.
  ([Tailscale SSH KB](https://tailscale.com/kb/1193/tailscale-ssh))

**Default policy caveat:** a new tailnet ships with only
```json
"ssh": [{ "action": "check", "src": ["autogroup:member"],
          "dst": ["autogroup:self"], "users": ["autogroup:nonroot", "root"] }]
```
This allows a user to SSH **only to their own devices** (`autogroup:self`). A **tagged**
device is owned by the tag, **not** by a user, so it is **not** `autogroup:self` for anyone —
meaning the default rule does **not** grant SSH into `tag:smith`. The user must add an
explicit `ssh` rule whose `dst` is `tag:smith`. ([Tailscale SSH KB](https://tailscale.com/kb/1193/tailscale-ssh),
[tags KB](https://tailscale.com/kb/1068/tags))

### Can the ACL be created by automation, or is it human/admin-owned?

Both are technically possible, but it is **tailnet-admin-owned config**:

- The whole policy file (ACLs, `ssh` block, `tagOwners`) **can** be set via API:
  `POST /api/v2/tailnet/{tailnet}/acl` — "The POST endpoint sets the ACL... HuJSON and JSON
  are both accepted." Supports `If-Match` for optimistic concurrency.
  ([Tailscale API](https://tailscale.com/kb/1101/api),
  [gitops-acl-action](https://github.com/tailscale/gitops-acl-action))
- **But** editing the policy file requires admin-level credentials (Owner/Admin/Network
  admin, or an OAuth client with the ACL write scope). Smith's per-VPS **auth key cannot**
  edit the policy — an auth key only enrolls a device. So in practice the `ssh` grant and
  `tagOwners` are **user-preconfigured** (or configured once by the user's own automation),
  not something smith should silently mutate on every bootstrap. Treat the policy as the
  user's to own.

### Role of tags and `tagOwners`

- The tag (`tag:smith`) is the **SSH destination** in the ACL `dst`, and the identity the
  device authenticates as. Applying a tag "removes any user-based authentication" — the
  device is owned by the tag, not a person. ([tags KB](https://tailscale.com/kb/1068/tags))
- A tag **must be declared in `tagOwners`** before anything can use it: "To apply a tag, it
  must already exist in the tailnet policy file." `tagOwners` lists who may apply it:
  ```json
  "tagOwners": { "tag:smith": ["autogroup:admin"] }
  ```
  An empty list (`"tag:smith": []`) still works — it's then implicitly owned by
  Owners/Admins/Network admins. The auth key can only carry a tag that its issuing entity is
  allowed to apply per `tagOwners`. ([tags KB](https://tailscale.com/kb/1068/tags))

---

## 3. Automate vs. pre-configure boundary

### What smith can do on the box **alone** (auth key only)

- `apt` install Tailscale from the official repo.
- `tailscale up --auth-key=... --hostname=... --advertise-tags=tag:smith --ssh` to enroll,
  name, tag, and enable Tailscale SSH — all non-interactive.
- Read local state via `tailscale status --json` / `tailscale ip -4`.
- Close the public SSH port after verification.

### What the **user must pre-configure in the tailnet** (smith cannot do with an auth key)

| Prerequisite | Why smith can't do it | Can smith **detect its absence**? |
|---|---|---|
| **`tagOwners` declares `tag:smith`** | Editing the policy file needs admin/ACL-write creds; the enrollment auth key can't. And a key referencing an undeclared/unowned tag is rejected. | **Yes, hard-fail at enrollment.** If the tag isn't owned, minting or using the key fails; `tailscale up` errors and the node never comes up (`BackendState` != `Running`, no tailnet IP). smith surfaces the `tailscale up` error verbatim. |
| **`ssh` ACL rule with `dst: tag:smith`** | Same — policy edit needs admin creds; also this is deliberately the user's security decision. | **Yes, via SSH probe.** The box enrolls fine and gets a tailnet IP, but the from-smith SSH-over-tailnet probe (step 4) fails with a Tailscale SSH permission/denied error. smith fails with "box enrolled but Tailscale SSH ACL does not grant access to tag:smith — add an `ssh` rule with dst tag:smith." |
| **MagicDNS enabled** (needed only if smith uses the `*.ts.net` name rather than the 100.x IP) | Tailnet-wide DNS setting, admin-owned. On by default for tailnets created ≥ 2022-10-20; older ones must enable it. ([MagicDNS KB](https://tailscale.com/kb/1081/magicdns)) | **Yes, soft.** `tailscale status --json` `Self.DNSName` is empty / non-resolvable when MagicDNS is off. smith can detect and **fall back to the 100.x IP** (`Self.TailscaleIPs[0]`) instead of failing — reachability does not require MagicDNS. |
| **Device approval** (if the tailnet enforces it) | Admin setting. | **Neutralized, not detected:** a **pre-authorized** auth key bypasses device approval, so smith doesn't need to detect it as long as it always mints pre-approved keys. ([auth-keys KB](https://tailscale.com/kb/1085/auth-keys)) |

**Design takeaway:** the two genuine human prerequisites are (1) `tagOwners` for `tag:smith`
and (2) an `ssh` rule with `dst: tag:smith`. Both are cleanly detectable — (1) fails
enrollment, (2) fails the SSH probe — so smith can give precise, actionable errors **before**
it ever closes the public port. MagicDNS is optional (fall back to IP). Device approval is
handled by pre-authorizing the key.

---

## 4. Verification before closing public SSH

smith needs two things confirmed: the box is **online on the tailnet**, and it is **reachable
over the tailnet from the smith machine** (which is itself a tailnet member).

### Fields exposed by `tailscale status --json`

Confirmed against the source of record, `ipn/ipnstate` (`ipnstate.go`); fields serialize
under their Go names (no custom json tags):
[ipnstate.go](https://github.com/tailscale/tailscale/blob/main/ipn/ipnstate/ipnstate.go)

- Top-level `BackendState` (string) — `"Running"` when tailscaled is up and authenticated.
- `Self` (`*PeerStatus`) and `Peer` (map of `*PeerStatus`), each `PeerStatus` with:
  - `Online` (bool) — control-plane online state.
  - `TailscaleIPs` (`[]netip.Addr`) — the 100.x/IPv6 tailnet addresses.
  - `DNSName` (string) — the MagicDNS FQDN, `host.tailnet-name.ts.net`.
  - `HostName` (string), `ID` (stable node ID).

### On the box (local self-check)

```shell
tailscale status --json     # assert .BackendState == "Running" and .Self.TailscaleIPs non-empty
tailscale ip -4             # prints the 100.x IPv4 directly ("Only return an IPv4 address")
```
`.Self.DNSName` gives the MagicDNS name (empty if MagicDNS is off — then use the IP).
([CLI reference](https://tailscale.com/kb/1080/cli), MagicDNS format from
[MagicDNS KB](https://tailscale.com/kb/1081/magicdns))

### From the smith machine (the real reachability proof)

Local `BackendState: Running` only proves the box thinks it's connected. The authoritative
check is from smith's own node:

1. `tailscale status --json` on the smith machine; find the VPS in `.Peer` by `HostName`/`ID`
   and read `Online == true`, plus its `TailscaleIPs[0]` (100.x) and `DNSName`.
2. **Actually SSH over the tailnet** to that IP or MagicDNS name — a real
   `ssh <user>@<100.x-or-DNSName>` probe. A successful command execution proves online +
   routed + Tailscale SSH ACL grants access, all at once. This single probe is the gate:
   only if it succeeds does smith close the public SSH port.

Using the 100.x IP from `TailscaleIPs` avoids any MagicDNS dependency; use `DNSName` only when
present. The SSH probe (not just the `Online` flag) is what distinguishes "enrolled but ACL
doesn't permit SSH" (issue-3 prerequisite #2) from genuinely reachable.

---

## Sources (primary)

- [Auth keys — tailscale.com/kb/1085](https://tailscale.com/kb/1085/auth-keys)
- [OAuth clients — tailscale.com/kb/1215](https://tailscale.com/kb/1215/oauth-clients)
- [Tailscale SSH — tailscale.com/kb/1193](https://tailscale.com/kb/1193/tailscale-ssh)
- [ACL tags — tailscale.com/kb/1068](https://tailscale.com/kb/1068/tags)
- [CLI reference — tailscale.com/kb/1080](https://tailscale.com/kb/1080/cli)
- [Tailscale API — tailscale.com/kb/1101](https://tailscale.com/kb/1101/api)
- [MagicDNS — tailscale.com/kb/1081](https://tailscale.com/kb/1081/magicdns)
- [Install on Linux — tailscale.com/docs/install/linux](https://tailscale.com/docs/install/linux)
- [Ubuntu Noble apt repo list](https://pkgs.tailscale.com/stable/ubuntu/noble.tailscale-keyring.list)
- [ipn/ipnstate source (status --json fields)](https://github.com/tailscale/tailscale/blob/main/ipn/ipnstate/ipnstate.go)
- [gitops-acl-action (ACL via API/automation)](https://github.com/tailscale/gitops-acl-action)

## Uncertainty / version notes

- Exact `POST /keys` request-body field names (`capabilities.devices.create.*`) are
  API-versioned; validate against the live API reference at bootstrap time.
- `tailscale status --json` fields have no explicit json tags, so they serialize by Go field
  name; a future rename would change the JSON. Pin to a known `tailscale` version or guard the
  parse.
- `tailscale set --ssh` (newer) vs `tailscale up --ssh` (during enrollment) both work; smith
  wants `--ssh` on the `up` call so enrollment and SSH-enable happen in one step.
