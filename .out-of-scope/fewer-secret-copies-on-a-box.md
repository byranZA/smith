# Fewer copies of a provisioned secret on a box

Smith does not try to keep a provisioned secret in fewer places on a box. A value such as `GH_TOKEN` stays in the staged env, in the mise fragment that exports it, and in the environment of the agent process that uses it.

## Why this is out of scope

Each copy does a job:

- `/etc/smith/env.json` is the staged env. On-box smith reads values from it by name and never resolves a reference itself.
- `~/.config/mise/conf.d/smith.toml`, and `~/workspace/<repo>/mise.toml` for a repo's overrides, export the values. Every session runs under `mise exec` and a plain SSH shell gets shims, never `mise activate`, so the generated mise config has to carry the env. It sits above every worktree, so no checkout can commit it.
- The agent process environment is how every coding agent reads its key.

The only copy that could go is the mise fragment's, by having it load values from a staged file (for example with mise's `_.file` env directive). That would mean reshaping the staged env or staging one file per scope, then re-checking that `mise exec` and the shims both see the values.

The gain does not cover that cost. [ADR-0009](../docs/adr/0009-provisioned-secrets-sit-in-plaintext.md) makes at-rest protection a stated non-goal, because the `smith` user has passwordless sudo and can read every copy anyway. Going from three copies to two narrows accidental exposure slightly and creates no boundary. The real controls stay scoped credentials and disposable boxes.

Secret storage on the operator's machine is a separate concern, and a secret-manager scheme (`op:`, `keychain:`) is the route there (#624).

## Prior requests

- #625: "Reduce the copies of a provisioned secret on a box"
