# PROTOTYPE — blueprint file format + ergonomics

Throwaway. Answers [smith#69](https://github.com/byranZA/smith/issues/69).
Not production code: no tests, no abstractions, no error wrapping.

The schema is locked by [#62](https://github.com/byranZA/smith/issues/62); the syntax
carrying it is not. The only way to judge a format is to read a real blueprint written
in it, so this writes the *same* non-trivial box — 3 repos, per-repo `tools` override,
box + repo placements, `env`, provider block — in each candidate, and runs a real
strict parser over both.

## Run it

    go run . candidates/acme.yaml candidates/acme.toml     # the good documents
    go run . candidates/broken.yaml candidates/broken.toml # the failure surface
    go run . candidates/footgun.toml candidates/footgun.yaml
    go run . candidates/preferences.yaml candidates/preferences-bad.yaml
    go run . resolve                                       # name selection

## What's here

    candidates/acme.{yaml,toml}         same box, both formats
    candidates/broken.{yaml,toml}       five failure classes at once
    candidates/footgun.{yaml,toml}      each format's own hazard
    candidates/preferences.{yaml,toml}  the fixed-key file
    candidates/preferences-bad.yaml     a collection where #62 forbids one
    home/                               a candidate ~/.smith/ on disk
    main.go, resolve.go                 the #62 schema + strict loaders + validation

## Findings

**1. Both formats carry the schema. Neither is disqualified.** Both parse to a
byte-identical tree. TOML nests `repos[].placements[]` fine via `[[repos.placements]]` —
the feared awkwardness is real but smaller than expected. The cost is density:
75 lines TOML vs 55 YAML for the same box, and the gap is entirely repeated
`[[repos.placements]]` headers where YAML has an inline `{ from, to, mode }`.

**2. The placement line is where the formats actually diverge.** A placement is four
short scalars and reads as one thought:

    - { from: "file:~/.secrets/acme/api.env", to: "packages/api/.env", mode: converge }

TOML's array-of-tables spends four lines and a header on it. TOML's inline-table syntax
recovers the one-liner, but then a blueprint mixes two table styles for no reason a
reader can infer.

**3. The strict-validation failure surface favours YAML, on line numbers.**

    # YAML
    line 3: field tool not found in type main.Blueprint
    line 12: field ref not found in type main.Repo

    # TOML
    unknown key(s): tool, tool.node, repos.ref

YAML's decoder reports **every** unknown key **with a line number**. BurntSushi's
`MetaData.Undecoded()` reports key paths with **no position**, and collapses the array
index — `repos.ref` does not say *which* repo. Both leak Go type names and need a
message pass regardless. This is a property of the Go libraries, not of the formats,
but it is the property an operator meets.

**4. Strict validation has a blind spot, and both formats share it exactly.**
`tools` and `env` are open maps, so nothing catches a key that lands in the wrong one:

    tools:
      node: "22"
      base: develop     # meant to be repo-level; silently becomes a tool named "base"

TOML's ordering rule produces the identical silent failure — a key written after
`[repos.tools]` belongs to `[repos.tools]`. **TOML's "unambiguous" reputation buys
nothing here.** Both need `mise`-name plausibility checking or nothing.

**5. #62's fixed-key preferences rule enforces itself for free.** A separate
`Preferences` struct with no collection fields makes `repos:` in preferences a parse
error, with no allowlist to maintain.

**6. Name selection is a real, unresolved collision.** #58 selects by bare name.
Once files carry extensions, `--blueprint prod` has to try candidates in some order:

    acme       → home/blueprints/acme.yaml
    prod       → home/blueprints/prod.yaml   ** AMBIGUOUS: also prod.toml **
    staging    → home/blueprints/staging/blueprint.yaml
    dev        → not found

Supporting two formats means a precedence rule, and a box whose marker records
`blueprint: prod` no longer identifies a file. Directory-per-blueprint (`staging/`)
resolves cleanly but adds a second layout to explain and buys nothing while a
blueprint is one document.
