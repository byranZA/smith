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

## Caveat

`adapters/*.json` and `fixtures/*.json` are written from vendor docs, not from
executed CLIs — the same MEDIUM-confidence gap #54 flagged. The offline check
proves the grammar reaches the shapes we *believe* are emitted. Only mode 3
proves the shapes.
