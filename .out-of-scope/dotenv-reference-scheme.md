# A dotenv reference scheme

Smith does not resolve a reference to one key inside a dotenv file, such as `dotenv:~/.secrets/api.env#GITHUB_TOKEN`. Each secret a blueprint stages is named by its own `file:` reference, or by `env:` when it really is meant to come from the operator's current shell.

## Why this is out of scope

The request comes from operators who keep their secrets in one `.env` file. That habit led them to source the file and use `env:` references, which goes wrong in both directions: forget to source it and `env:` silently stages the shell's own, usually broader, token onto the box; source it and the terminal goes on carrying the box's credentials ([`docs/blueprints.md`](../docs/blueprints.md), "Prefer `file:` over `env:`").

`file:` already fixes that. It depends on nothing in the shell, and the tested example blueprint keeps one small file per secret:

```yaml
env:
  GH_TOKEN: file:~/.secrets/loop-box/gh-token
  CLAUDE_CODE_OAUTH_TOKEN: file:~/.secrets/loop-box/claude-token
```

So a `dotenv:` scheme would only fold several files into one. For that convenience smith would take on a dotenv parser, and dotenv is ambiguous in exactly the places that matter: `export` prefixes, quoting, multi-line values and `#` inside values. [ADR-0009](../docs/adr/0009-provisioned-secrets-sit-in-plaintext.md) named that cost when it first set the scheme aside.

Keeping many secrets in one place is a secret manager's job. When smith needs it, it will add a secret-manager scheme (`op:`, `keychain:`, `pass:` and the like) in the same `scheme:arg` slot that `internal/secret` leaves open, and skip the dotenv step.

A whole-file import such as `env_file: <path>` stays out too, for a stronger reason: it would stage whatever the file happens to hold, so a secret added to the file for an unrelated purpose would silently land on every box built from the blueprint.

## Prior requests

- #296: "A dotenv: reference scheme: many secrets in one file, named one by one"
