# PROTOTYPE — provider adapter against one real CLI

Throwaway spike for [issue #60](https://github.com/byranZA/smith/issues/60). Not production code.

Tests one claim: a provider adapter can be **three command templates + field
extractors**, with no provider SDK and no provider-specific code in smith.
`main.go` therefore contains zero provider knowledge — every provider fact lives
in `adapters/*.json`. If a step ever needs `if adapter.Name == ...`, the contract
has failed.

## The three modes

```bash
# 1. Free. Validates the extractor grammar against fixtures/. No token, no spend.
go run ./prototype/provideradapter -adapter prototype/provideradapter/adapters/doctl.json -offline

# 2. Free. Prints the three commands fully expanded. Read this before spending.
go run ./prototype/provideradapter -adapter prototype/provideradapter/adapters/doctl.json \
  -dry-run -image ubuntu-24-04-x64 -size s-1vcpu-1gb -region ams3 -sshkey <key-id>

# 3. COSTS MONEY. Creates a real box, then destroys it.
go run ./prototype/provideradapter -adapter prototype/provideradapter/adapters/doctl.json \
  -image ubuntu-24-04-x64 -size s-1vcpu-1gb -region ams3 -sshkey <key-id> -raw
```

The live run walks: create → extract id (+ maybe IP) → poll `list` for the IP if
deferred → poll port 22 for an sshd banner → `list` by tag with a client-side
filter → **forget the id and resolve it back from the IP alone** → destroy →
`list` to confirm it is gone.

`-keep` skips the destroy and leaves a billing box behind. Any failure after
create prints a leak warning with the destroy command.

## Verdict

The contract holds. A full lifecycle ran against real `doctl` 1.166.0 — create →
IP → sshd banner in 15s → list → resolve IP back to id → destroy → confirm — with
`main.go` containing no provider knowledge. Total box lifetime ~40s.

What the live run changed:

- **Tagging is a separate permission from creating.** A droplet-scoped DO token
  gets `403 tag:create` on `--tag-name`. smith must not assume native tags.
- **A name convention substitutes for tags with zero harness change** — only a
  different adapter file (`doctl-nametag.json`, tag path `name`). This is the
  strongest evidence the contract is at the right altitude: tag-vs-name is an
  adapter concern, not smith's.
- **Destroy is async.** `list` immediately after a successful `delete` still
  returns the box. Teardown needs a confirmation poll; a zero exit is not proof.
- **`tags` is `null`, not `[]`,** when a droplet is untagged.
- **`networks.v4` ordering is not stable** — public came second in one droplet
  and first in another, so the `[type=public]` predicate is load-bearing.
  A positional path would hand smith a `10.x` address.
- **A flat argv list can't express an optional argument** (see `expandAll`).

`adapters/hcloud.json` and its fixtures remain **unverified** — written from
vendor docs, no `hcloud` executed. The `doctl` fixtures now match real captured
output with values redacted (this repo is public).

Not validated: the pre-registered **SSH-key reference** at create — the token
lacked `ssh_key` scope, so the run created a box with no key.
