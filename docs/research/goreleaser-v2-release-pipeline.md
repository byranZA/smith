# GoReleaser v2 config schema + GitHub build-provenance wiring

Research for GitHub issue [#36](https://github.com/byranZA/smith/issues/36) (part of the
release & distribution map, [#33](https://github.com/byranZA/smith/issues/33)).

Everything below was checked against **primary sources today** — the GoReleaser docs *and*
the GoReleaser source that implements them, the `actions/attest` / `actions/attest-build-provenance`
action definitions, and Go's own `cmd/link` documentation. Nothing here is recalled
defaults; where the docs and the code disagree, the code is quoted.

Versions in force at the time of writing:

| Thing | Current | Checked via |
|---|---|---|
| GoReleaser | **v2.17.0** | `gh api repos/goreleaser/goreleaser/releases/latest` |
| `goreleaser/goreleaser-action` | **v7** (v7.2.3) | `gh api repos/goreleaser/goreleaser-action/releases/latest` |
| `actions/attest-build-provenance` | **v4** (v4.1.1) | `gh api repos/actions/attest-build-provenance/releases/latest` |
| `actions/attest` | **v4** (v4.1.1) | referenced by the wrapper's `action.yml` |
| `actions/checkout` | **v7** (v7.0.1) | `gh api repos/actions/checkout/releases/latest` |
| `actions/setup-go` | **v7** (v7.0.0) | `gh api repos/actions/setup-go/releases/latest` |

> **Config must start with `version: 2`.** GoReleaser warns
> `only version: 2 configuration files are supported, yours is version: 0` when the key is
> missing, and turns that warning into a hard error if strict unmarshalling also fails.
> Source: [`pkg/config/load.go`](https://github.com/goreleaser/goreleaser/blob/main/pkg/config/load.go)
> (`validVersion := versioned.Version == 2`).

---

## 1. `archives` schema in v2

**ANSWER**

- The key is **`formats`** (a list). The singular **`format` is deprecated** — supplying it
  emits a deprecation notice and is folded into `formats`.
- Per-OS override key is **`format_overrides`** (singular "format", plural "overrides"), a
  list of `{goos, formats}` entries. One archives entry + one override is all that is needed.
- **Default `formats` is `['tar.gz']`**, so `tar.gz` for darwin+linux comes for free; only
  windows needs the override.
- The **default `name_template` already produces the target name** for smith's matrix.

Evidence:

```yaml
# Default: ['tar.gz'].
format: "zip"                 # Singular form, single format, deprecated.
formats: ["zip", "tar.gz"]    # Plural form, multiple formats. (since v2.6)

# Can be used to change the archive formats for specific GOOSs.
# Most common use case is to archive as zip on Windows.
format_overrides:
  - goos: windows
    formats: ["zip", "tar.gz"]
```

Source: <https://goreleaser.com/customization/package/archives/>

The deprecation is not just a docs note — it is enforced in code:

```go
if archive.Format != "" {
    deprecate.Notice(ctx, "archives.format")
    archive.Formats = append(archive.Formats, archive.Format)
}
if len(archive.Formats) == 0 {
    archive.Formats = []string{"tar.gz"}
}
```

Source: [`internal/pipe/archive/archive.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/archive/archive.go).
The struct tags confirm the YAML keys and mark `format` / `builds` deprecated in favour of
`formats` / `ids`:
[`pkg/config/config.go` — `type Archive struct`](https://github.com/goreleaser/goreleaser/blob/main/pkg/config/config.go).

### Default vs explicit `name_template`

Default (from the docs, and from `defaultNameTemplate` in `archive.go`):

```
{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}{{ with .Arm }}v{{ . }}{{ end }}{{ with .Mips }}_{{ . }}{{ end }}{{ if not (eq .Amd64 "v1") }}{{ .Amd64 }}{{ end }}
```

`.Os` is literally `GOOS` and `.Arch` is literally `GOARCH`
(<https://goreleaser.com/customization/general/templates/>), and the v1 `replacements`
key that used to title-case them **no longer exists in v2** (it survives only in the v1.14 /
v1.19 blog posts). smith's matrix has no `arm`, no `mips`, and only `GOAMD64=v1`, so every
conditional segment collapses to empty and the default renders exactly:

```
smith_0.1.0_darwin_arm64.tar.gz
```

So the map's settled asset name is the *default* behaviour. The draft config below still
writes it out explicitly — the goal is a locked asset-name contract for a later bootstrap
map, and an explicit template cannot drift with a future default change.

---

## 2. `{{ .Version }}` semantics

**ANSWER** — `.Version` is the tag with a leading `v` stripped. The **raw tag is `.Tag`**
(`.Summary`, `.PreviousTag` are also raw). Under `--snapshot`/`--nightly`, `.Version` is
replaced by the snapshot/nightly version (see fact 6).

Evidence — the templates table plus its footnote:

| Key | Description |
|---|---|
| `.Version` | the version being released[^version-prefix] |
| `.Tag` | the current git tag |

> [^version-prefix]: The `v` prefix is stripped, and it might be changed in `snapshot` and
> `nightly` builds.

Source: <https://goreleaser.com/customization/general/templates/>

Confirmed in code — it is a plain `TrimPrefix`, not a semver reformat:

```go
ctx.Version = strings.TrimPrefix(ctx.Git.CurrentTag, "v")
```

Source: [`internal/pipe/git/git.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/git/git.go).

So `v0.1.0` → `0.1.0`, and `v0.1.0-rc.1` → `0.1.0-rc.1`. This is exactly the map's
"tags carry `v`, nothing else does" rule, for free.

---

## 3. `builds` schema in v2

**ANSWER** — all seven keys the map needs are unchanged and valid in v2:
`main`, `binary`, `env`, `flags`, `ldflags`, `goos`, `goarch`, `ignore`, `mod_timestamp`.
The v2 docs moved to a per-language *builder* page; the Go builder is the default
(`builder: go`), so no `builder` key is required.

Evidence (all quoted from the same annotated example):

| Key | Doc text | Default |
|---|---|---|
| `main` | "Path to main.go file or main package." | `.` |
| `binary` | "Binary name. Can be a path (e.g. `bin/app`) to wrap the binary in a directory." | project directory name |
| `env` | "Custom environment variables to be set during the builds." | `os.Environ()` ++ `env` config section |
| `flags` | "Custom flags." Templates allowed. | — |
| `ldflags` | "Custom ldflags." | `-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}} -X main.builtBy=goreleaser` |
| `goos` | "GOOS list to build for." | `[ 'darwin', 'linux', 'windows' ]` |
| `goarch` | "GOARCH to build for." | `[ '386', 'amd64', 'arm64' ]` |
| `ignore` | "List of combinations of GOOS + GOARCH + GOARM to ignore." (list of `{goos, goarch, …}`) | — |
| `mod_timestamp` | "Set the modified timestamp on the output binary, typically you would do this to ensure a build was reproducible." | empty = compile time |

Source: <https://goreleaser.com/customization/builds/builders/go/>

Two consequences worth flagging:

1. **Setting `ldflags` replaces the default entirely.** The default injects
   `main.version` / `main.commit` / `main.date` / `main.builtBy`. smith has none of those
   symbols (`cmd/smith/main.go` is a one-liner and the var lives in `internal/cli`), so the
   custom `ldflags` must carry everything smith wants — which is just `buildVersion`.
2. **Reproducibility guidance from the same page:** set `mod_timestamp` to
   `{{ .CommitTimestamp }}`, pass `-trimpath` in `flags`, and avoid `{{ .Date }}` /
   the `time` function in ldflags. The draft below does all three.

`ignore` is the right way to drop `windows/arm64` from a `goos × goarch` matrix — the
alternative (`targets:`) "overrides `goos`, `goarch`, … and `ignores`" and would mean
spelling out full target strings including the `_v1` GOAMD64 suffix, which is more brittle.

---

## 4. `before.hooks`, `checksum.name_template`, `changelog`, `release.prerelease`

**ANSWER** — all unchanged in v2 and all valid as written in the map.

### `before.hooks`

```yaml
before:
  hooks:
    - make clean
    - go generate ./...
    - go mod tidy
```

"The `before` section allows for global hooks that will be executed **before** the release
is started. … Note that if any of the hooks fails the release process is aborted."
Source: <https://goreleaser.com/customization/general/hooks/>

(The `cmd:`/`dir:`/`env:`/`if:` object form is **Pro-only**; the OSS form is a plain list of
command strings. `go test ./...` as a bare string is fine.)

### `checksum.name_template`

```yaml
checksum:
  # Default: '{{ .ProjectName }}_{{ .Version }}_checksums.txt'
  name_template: "{{ .ProjectName }}_checksums.txt"
  # Default: 'sha256'.
  algorithm: sha256
```

Source: <https://goreleaser.com/customization/package/checksum/>

Note the default is **`smith_0.1.0_checksums.txt`**, not `checksums.txt`. The map settled on
`checksums.txt`, which also happens to be what the attestation wiring wants (fact 5) — so
`name_template: "checksums.txt"` is required, not cosmetic.

### `changelog`

```yaml
changelog:
  use: github        # Default: 'git'. Valid: git | github | gitlab | gitea | github-native
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - typo
```

Source: <https://goreleaser.com/customization/publish/changelog/>

Caveats from that page, both relevant to smith: `github-native` disables groups/sort/filters
(so do **not** use it if the `docs:`/`test:`/`chore:` filters matter), and "The `github`
changelog will only work if both tags exist in GitHub" — i.e. on the very first tag there is
no previous tag to compare against.

### `release.prerelease: auto`

```yaml
release:
  # If set to auto, will mark the release as not ready for production
  # in case there is an indicator for this in the tag e.g. v1.0.0-rc1
  # If set to true, will mark the release as not ready for production.
  # Default: false.
  prerelease: auto
```

Source: <https://goreleaser.com/customization/publish/scm/>

**Does it fire for `v0.1.0-rc.1`? Yes.** The "indicator" is not a substring match — it is the
parsed semver prerelease segment:

```go
switch ctx.Config.Release.Prerelease {
case "auto":
    if ctx.Semver.Prerelease != "" {
        ctx.PreRelease = true
    }
```

Source: [`internal/pipe/release/release.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/release/release.go)

and `ctx.Semver` comes from `Masterminds/semver/v3` parsing the raw tag:

```go
sv, err := semver.NewVersion(ctx.Git.CurrentTag)
...
ctx.Semver = context.Semver{ Major: ..., Prerelease: sv.Prerelease() }
```

Source: [`internal/pipe/semver/semver.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/semver/semver.go)

`v0.1.0-rc.1` parses with `Prerelease() == "rc.1"` → non-empty → the GitHub release is marked
prerelease. Note the same pipe means a **non-semver tag is a hard failure**
(`failed to parse tag '%s' as semver`), which is a useful guardrail for the map's tagging rule.

---

## 5. Composing `attest-build-provenance` with `goreleaser-action`

**ANSWER**

- **Separate step, after GoReleaser, in the same job.** GoReleaser has no built-in attestation
  step in OSS; the attestation action runs afterwards over what GoReleaser left in `dist/`.
- **Do not glob `dist/`. Point at the checksums file** — `subject-checksums: ./dist/checksums.txt`.
  Every file listed in the checksums file becomes a subject, which is exactly the set of
  published assets and nothing else (`dist/` also holds per-target binary subdirectories,
  `artifacts.json`, `metadata.json`, `config.yaml`). A `subject-path` glob such as
  `dist/*.tar.gz` works but has to be maintained in step with the archive formats.
- **Job permissions: `contents: write` + `id-token: write` + `attestations: write`.**
  `contents: write` is GoReleaser's (create release, upload assets); the other two are the
  attestation's.
- **Current majors: `goreleaser/goreleaser-action@v7`, `actions/attest-build-provenance@v4`.**

Evidence — GoReleaser's own attestation page shows the exact composition:

```yaml
permissions:
  # Give the workflow permission to write attestations.
  id-token: write
  attestations: write

jobs:
  release:
    steps:
      - uses: goreleaser/goreleaser-action@v7
      # After GoReleaser runs, attest all the files in ./dist/checksums.txt:
      - uses: actions/attest@v4
        with:
          subject-checksums: ./dist/checksums.txt
```

…together with the required config change:

```yaml
checksum:
  name_template: "checksums.txt"
```

Source: <https://goreleaser.com/customization/publish/attestations/>

GitHub's own docs give the same permission set for attesting binaries:

```yaml
permissions:
  id-token: write
  contents: read
  attestations: write
```

Source: <https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations>
(smith needs `contents: **write**` instead of `read`, because the same job publishes the
GitHub Release.)

The `id-token` permission "gives the action the ability to mint the OIDC token necessary to
request a Sigstore signing certificate. The `attestations` permission is necessary to persist
the attestation."
Source: <https://github.com/actions/attest#usage>

### ⚠️ `attest-build-provenance` v4 is now a wrapper

> "**As of version 4, `actions/attest-build-provenance` is simply a wrapper on top of
> `actions/attest`.** Existing applications may continue to use the `attest-build-provenance`
> action, but new implementations should use `actions/attest` instead."

Source: <https://github.com/actions/attest-build-provenance#usage>

Its `action.yml` at v4.1.1 is literally a composite step delegating to
`actions/attest@…  # v4.1.1`, passing `subject-path`, `subject-checksums`, `subject-digest`,
etc. straight through — so both spellings accept identical inputs and behave identically.
Source: [`actions/attest-build-provenance/action.yml @ v4.1.1`](https://github.com/actions/attest-build-provenance/blob/v4.1.1/action.yml)

**Recommendation:** use `actions/attest@v4` — it is what GoReleaser's docs now show, it is
what the wrapper's own README tells new implementations to use, and it removes one layer of
indirection. This is a *deviation from the ticket's wording* (`attest-build-provenance`), so
it is called out here rather than made silently; swapping the `uses:` line back is a
one-word change if the map prefers the wrapper name.

`actions/attest` defaults to provenance mode when no `sbom-path` / predicate inputs are
given ("Provenance | No `sbom-path` or predicate inputs | Auto-generates SLSA build
provenance"), so no `predicate-type` is needed.
Source: <https://github.com/actions/attest#attestation-modes>

### Two caveats

1. **`artifact-metadata: write`** appears in the `actions/attest` README's numbered setup
   ("necessary to create the artifact storage record") but **not** in its own binary/provenance
   example, and storage records only apply with `push-to-registry: true` (container images).
   smith publishes no images, so it is omitted below. *Not fully verified* — if a future run
   logs a permission error mentioning storage records, add it.
2. **Repo visibility.** "Artifact attestations are available in public repositories for all
   current GitHub plans… To use artifact attestations in private or internal repositories,
   you must be on a GitHub Enterprise Cloud plan."
   `byranZA/smith` is **public** (`gh repo view byranZA/smith --json visibility` → `PUBLIC`),
   so this is free and available. If the repo were ever made private, this step breaks.

Verification for a user, once published:
`gh attestation verify --owner byranZA smith_0.1.0_darwin_arm64.tar.gz`
(Source: same GoReleaser attestations page.)

---

## 6. Git state requirements, and what `--snapshot` substitutes

**ANSWER** — for a real (non-snapshot) release GoReleaser requires, in order:

1. **A git repository** (`ErrNotRepository` otherwise).
2. **A tag it can resolve**, and **HEAD must be exactly at that tag**.
3. **A clean working tree** — `git status --porcelain` must be empty.
4. **A tag parseable as semver.**

`fetch-depth: 0` is needed because the default shallow `actions/checkout` clone has neither
the tag history nor the previous tag the changelog compares against.

Evidence — the validation is four lines of git:

```go
func validate(ctx *context.Context) error {
    if ctx.Snapshot { return pipe.ErrSnapshotEnabled }
    if skips.Any(ctx, skips.Validate) { return pipe.ErrSkipValidateEnabled }
    if _, err := os.Stat(".git/shallow"); err == nil {
        log.Warn("running against a shallow clone - check your CI documentation at https://goreleaser.com/ci")
    }
    if err := CheckDirty(ctx); err != nil { return err }
    _, err := git.Clean(git.Run(ctx, "describe", "--exact-match", "--tags", "--match", ctx.Git.CurrentTag))
    if err != nil { return ErrWrongRef{...} }
    return nil
}

func CheckDirty(ctx *context.Context) error {
    out, err := git.Run(ctx, "status", "--porcelain")
    if strings.TrimSpace(out) != "" || err != nil { return ErrDirty{status: out} }
    return nil
}
```

Source: [`internal/pipe/git/git.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/git/git.go)

Note the first line: **`--snapshot` short-circuits every one of those checks.** A local
`goreleaser release --snapshot --clean` therefore builds happily on a dirty tree with no tag —
which is precisely why the map can call it "locally verifiable", but also why a green snapshot
run does **not** prove the tag-time validation will pass.

And the docs state the CI requirement directly:

> "Notice the `fetch-depth: 0` option in the `Checkout` workflow step. This is required for
> GoReleaser to work properly, as it will need the full history to work properly."

Source: <https://goreleaser.com/customization/ci/actions/>

This is also why the map's "no `go mod tidy` in `before` hooks" rule is more than hygiene: a
hook that rewrites `go.mod`/`go.sum` dirties the tree *after* validation has already run, so
the published binary can be built from a tree that no longer matches the tag — silently.

### What `--snapshot` substitutes for the version

`ctx.Version` is replaced by the rendered `snapshot.version_template`:

```go
if ctx.Config.Snapshot.VersionTemplate == "" {
    ctx.Config.Snapshot.VersionTemplate = "{{ .Version }}-SNAPSHOT-{{ .ShortCommit }}"
}
...
ctx.Version = name
```

Source: [`internal/pipe/snapshot/snapshot.go`](https://github.com/goreleaser/goreleaser/blob/main/internal/pipe/snapshot/snapshot.go)
and <https://goreleaser.com/customization/publish/snapshots/>

So a snapshot on a repo whose latest tag is `v0.1.0` produces `0.1.0-SNAPSHOT-abc1234`, and a
repo with **no tags at all** falls back to `CurrentTag = "v0.0.0"` → `0.0.0-SNAPSHOT-abc1234`
(`internal/pipe/git/git.go`, `ErrNoTag` branch). Assets and the ldflags-injected
`buildVersion` all carry that string. Also note the key is **`version_template`** in v2 —
`snapshot.name_template` is deprecated (`deprecate.Notice(ctx, "snapshot.name_template")`),
even though the prose on the docs page still says "name_template" in one sentence.

Snapshot artifacts are never uploaded: "Artifacts won't be uploaded and will only be generated
into the `dist` directory."

---

## 7. Can `-X` set an unexported var in an `internal` package?

**ANSWER — yes. Verified twice: by the linker docs, and empirically in this repo.**

Go's linker documentation:

```
-X importpath.name=value
	Set the value of the string variable in importpath named name to value.
	This is only effective if the variable is declared in the source code either uninitialized
	or initialized to a constant string expression. -X will not work if the initializer makes
	a function call or refers to other variables.
	Note that before Go 1.5 this option took two separate arguments.
```

Source: <https://pkg.go.dev/cmd/link> (identical to local `go doc cmd/link`).

The constraints are about **the variable's type and initializer**, not its export status or
package location:

- must be a **string** variable — `buildVersion` is;
- must be **uninitialized or initialized to a constant string expression** —
  `var buildVersion = "0.0.0-dev"` is a constant string literal, so it qualifies;
- must not be `const` (a `const` is inlined at compile time and cannot be patched) — it is a
  `var`, per the doc comment already in `internal/cli/cli.go`.

Nothing in the docs mentions unexported identifiers or `internal/`, and `internal/` is a
**compiler**-enforced import restriction, not a linker one — the linker works on fully
qualified symbol names after compilation.

**Empirical check run against this worktree** (Go 1.26, module path currently
`github.com/byran/smith`):

```
$ go test ./internal/cli -run TestZZLdflagsProbe -v \
    -ldflags "-s -w -X github.com/byran/smith/internal/cli.buildVersion=0.1.0"
=== RUN   TestZZLdflagsProbe
    zz_ldflags_probe_test.go:7: buildVersion=0.1.0
--- PASS: TestZZLdflagsProbe (0.00s)

$ go build -trimpath \
    -ldflags "-s -w -X github.com/byran/smith/internal/cli.buildVersion=0.1.0-probe" \
    -o /tmp/smithprobe ./cmd/smith
$ strings /tmp/smithprobe | grep -c "0.1.0-probe"
1
```

(The probe test file was temporary and has been removed; the worktree is clean.)

So `-X github.com/byranZA/smith/internal/cli.buildVersion={{ .Version }}` is valid — **once
the module path is actually `github.com/byranZA/smith`**. See the contradiction note below.

---

## ⚠️ Contradiction with the map's settled constraints

**`go.mod` currently declares `module github.com/byran/smith`, not `github.com/byranZA/smith`.**
The map already knows this and fixes it in a sibling ticket, but the ordering matters and is
worth stating plainly: **the ldflags path must match `go.mod` exactly at build time.** A
mismatch is *silent* — `-X` on a symbol that does not exist is not an error; the binary just
keeps reporting `0.0.0-dev`. The `.goreleaser.yaml` below is written against the *post-rename*
path, so **it must not be merged before the module rename lands**, or the very first release
will ship a binary that lies about its version and nothing will fail.

Everything else in the map's settled framing is confirmed by the sources above:
`{{ .Version }}` strips the `v`; `prerelease: auto` fires on `v0.1.0-rc.1`; archives default to
`tar.gz` with `zip` needing only a `format_overrides` entry; the default archive name is already
`smith_0.1.0_darwin_arm64.tar.gz`; `go mod tidy` in `before` hooks really can dirty the tree
after validation; attestation needs only the automatic `GITHUB_TOKEN` plus two extra
permissions (zero hand-created secrets).

### Not verified

- **`goreleaser check` was not run** — GoReleaser is not installed on this machine
  (`which goreleaser` → not found). The draft below is schema-checked against the v2 config
  structs and the annotated docs by hand, not by the tool. **Run `goreleaser check` and
  `goreleaser release --snapshot --clean` before merging** — that is the map's own
  local-verifiability claim and it costs one command.
- **`artifact-metadata: write`** — see fact 5, caveat 1.
- **First-tag changelog behaviour** — `use: github` compares two tags; what it renders when
  `v0.1.0-rc.1` is the first tag ever is not documented precisely enough to state here.
  The map already parks changelog quality under "not yet specified".

---

## Draft `.goreleaser.yaml`

Ready to review. Matches the map's settled constraints; every key is cited above.

```yaml
# yaml-language-server: $schema=https://goreleaser.com/static/schema.json
version: 2

project_name: smith

# Fact 4: OSS `before` hooks are a plain list of command strings, and a failing
# hook aborts the release. Deliberately NO `go mod tidy` (fact 6): it mutates
# go.mod/go.sum after the clean-tree check has already passed.
before:
  hooks:
    - go test ./...

builds:
  - id: smith
    main: ./cmd/smith
    binary: smith
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    # Replaces GoReleaser's default ldflags entirely (fact 3) — smith has no
    # main.version/commit/date/builtBy symbols to fill.
    ldflags:
      - -s -w -X github.com/byranZA/smith/internal/cli.buildVersion={{ .Version }}
    # Reproducible builds: pin the binary mtime to the commit, not build time.
    mod_timestamp: "{{ .CommitTimestamp }}"
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    # 5 targets, not 6: no windows/arm64.
    ignore:
      - goos: windows
        goarch: arm64

archives:
  - id: smith
    # `formats` (plural) — `format` is deprecated in v2 (fact 1).
    formats:
      - tar.gz
    format_overrides:
      - goos: windows
        formats:
          - zip
    # Equal to the v2 default for this matrix; written out to lock the asset-name
    # contract the bootstrap map will depend on.
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    # `files` intentionally omitted: the default globs already pick up
    # LICENSE* and README*.

checksum:
  # Default would be smith_0.1.0_checksums.txt; the attestation step needs a
  # predictable path (fact 5).
  name_template: "checksums.txt"
  algorithm: sha256

changelog:
  use: github
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
      - "^ci:"

release:
  # v0.1.0-rc.1 -> semver prerelease "rc.1" -> marked prerelease (fact 4).
  prerelease: auto
```

## Draft `.github/workflows/release.yml`

```yaml
name: release

on:
  push:
    tags:
      - "v*"

# Least privilege at the top; the release job widens what it needs.
permissions:
  contents: read

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write # create the release and upload assets (GoReleaser)
      id-token: write # mint the Sigstore OIDC token (attestation)
      attestations: write # persist the attestation (attestation)
    steps:
      - name: Checkout
        uses: actions/checkout@v7
        with:
          # Required: GoReleaser needs full history + tags (fact 6).
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          # Release with the toolchain the module declares, not "stable".
          go-version-file: go.mod

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          # Provided automatically by Actions; no hand-created secret needed.
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Attest build provenance
        # v4 wrapper `actions/attest-build-provenance@v4` is equivalent; its own
        # README points new implementations here (fact 5).
        uses: actions/attest@v4
        with:
          # Every artifact listed in checksums.txt becomes a subject.
          subject-checksums: ./dist/checksums.txt
```

`go-version-file` accepts "Path to the go.mod, go.work, .go-version, or .tool-versions file"
— source: [`actions/setup-go/action.yml @ v7.0.0`](https://github.com/actions/setup-go/blob/v7.0.0/action.yml).
The `goreleaser-action` inputs used above (`distribution`, `version`, `args`) and the
`version` default of `~> v2` are from
[`goreleaser-action/action.yml @ v7.2.3`](https://github.com/goreleaser/goreleaser-action/blob/v7.2.3/action.yml).
