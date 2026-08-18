# PROTOTYPE — blueprint file format + ergonomics

Throwaway. Answers [smith#69](https://github.com/byranZA/smith/issues/69).
Not production code: no tests, no abstractions, no error wrapping.

The schema is locked by [#62](https://github.com/byranZA/smith/issues/62); the syntax
carrying it was not. The only way to judge a format is to read a real blueprint written
in it, so this writes the *same* non-trivial box — 3 repos, per-repo `tools` override,
box + repo placements, `env`, provider block — in each candidate, and runs a real strict
parser over both.

## Run it

    go run . candidates/acme.yaml candidates/acme.toml     # the good documents
    go run . candidates/broken.yaml candidates/broken.toml # the failure surface
    go run . candidates/footgun.yaml candidates/footgun.toml
    go run . candidates/scope.yaml                         # the repo-scope path rule
    go run . candidates/preferences.yaml candidates/preferences-bad.yaml
    go run . resolve                                       # name selection

## Verdict

**YAML, one format, file-per-blueprint** — `~/.smith/blueprints/<name>.yaml`, extension
not part of the name. House style is **uniform expansion**. Two validation rules were
added on top of #62's strict pass, both proven here.

## Findings

**1. Both formats carry the schema; neither was disqualified on capability.** Both parse
to a byte-identical tree. TOML nests `repos[].placements[]` fine via `[[repos.placements]]` —
the feared awkwardness is real but smaller than expected. The cost is density: 75 lines
TOML vs 55 YAML (inline) for the same box, the gap being repeated table headers.

**2. The strict-validation failure surface favours YAML, on line numbers.**

    # YAML
    line 3: field tool not found in type main.Blueprint
    line 12: field ref not found in type main.Repo

    # TOML
    unknown key(s): tool, tool.node, repos.ref

yaml.v3's `KnownFields` reports **every** unknown key **with a line number**. BurntSushi's
`MetaData.Undecoded()` reports key paths with **no position** and collapses the array index —
`repos.ref` does not say *which* repo. A property of the Go libraries, not the formats, but
it is the property an operator meets. Both leak Go type names and need a message pass.

**3. Strict validation has a blind spot, and both formats shared it exactly.** `tools` and
`env` are open maps, so nothing caught a key landing in the wrong one:

    tools:
      node: "22"
      base: develop     # meant repo-level, one indent too deep

TOML produced the identical silent failure via its ordering rule — a key after
`[repos.tools]` belongs to `[repos.tools]`. **TOML's "unambiguous" reputation bought nothing
at the place the schema is actually open.** Closed by rule 1 below.

**4. Scope-by-declaration-site is invisible in the expanded form.** Inline, box and repo
placements were different visual shapes; expanded they are the same shape at different
indents, and the `repos:` header that establishes scope can be twenty lines up. Nothing in
the schema stopped a repo placement writing `to: ~/.npmrc`, which lands silently in the
worktree. Closed by rule 2 below.

**5. #62's fixed-key preferences rule enforces itself for free.** A separate `Preferences`
struct with no collection fields makes `repos:` in preferences a parse error, with no
allowlist to maintain.

**6. Multi-format support was the source of the name-selection collision.** With two
formats, `--blueprint prod` finds both `prod.yaml` and `prod.toml`, needs a precedence
rule, and a marker recording `blueprint: prod` stops identifying a file. Choosing one
format dissolves all three. `go run . resolve` demonstrates the collision.

## The two rules

**Rule 1 — schema field names are reserved inside the open maps.** A key named `url`,
`base`, `mode`, `from`, `to`, … under `tools`/`env` is an error. **Case-sensitive**: schema
fields are lowercase, so a real env var named `URL` or `MODE` is untouched. Offline, no
`mise` lookup, catches exactly the misindent class.

    repos[0].tools.base: "base" is a schema field name and is reserved here — check the indentation

**Rule 2 — placement `to:` must match its scope.** Repo-scoped is worktree-relative, box-scoped
is absolute or `~`-relative. Turns #62's convention into a validation rule.

    placements[0].to: "etc/gitconfig" is relative, but a box placement needs an absolute or ~-relative path
    repos[0].placements[0].to: "~/.npmrc" is absolute, but a repo placement is worktree-relative
                               — declare it at box scope to write outside the worktree

## What's here

    candidates/acme.yaml                the verdict document — YAML, uniform expansion
    candidates/acme.toml                the rejected candidate, same box
    candidates/broken.{yaml,toml}       five failure classes at once
    candidates/footgun.{yaml,toml}      the open-map blind spot, both formats
    candidates/scope.yaml               both halves of rule 2
    candidates/preferences.{yaml,toml}  the fixed-key file
    candidates/preferences-bad.yaml     a collection where #62 forbids one
    home/                               a candidate ~/.smith/ on disk
    main.go, resolve.go                 the #62 schema + strict loaders + validation
