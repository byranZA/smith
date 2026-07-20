> **Note to the receiving agent:** This document is a *guide*, not ground truth.
> Everything below — including decisions, proposals, and suggested next steps — is the
> previous agent's best summary of the prior session. Before acting on any specific
> claim (file paths, function names, decisions, "the user said X"), verify it against
> the current state of the code, the user's stated goals, and your own judgment. Treat
> suggestions as starting points to discuss with the user, not commitments. If anything
> here conflicts with what you observe now, trust what you observe and flag the conflict.

# Hand-off: Release & Distribution (GoReleaser + GitHub Releases)

**Date:** 2026-07-20
**Source session:** Free-form planning discussion — how to build and distribute the `smith` binary to users via GitHub.

## One-line summary
Planned a tag-triggered GitHub Actions + GoReleaser pipeline that publishes cross-compiled `smith` binaries to GitHub Releases, plus the design for smith bootstrapping a version-matched Linux build onto the VPS.

## Goal
**Immediate:** stand up an automated release pipeline so users can download prebuilt `smith` binaries from GitHub — no dedicated website or object storage.
**Broader:** support smith's self-bootstrap story, where local smith (Mac/Windows) installs the matching Linux smith onto a fresh VPS.

## Context the next agent needs
- **smith** turns a fresh VPS into a remote dev machine and runs a delegation loop for AFK coding agents. It's a **single static Go binary, cross-compiled, no host daemon** (see `AGENTS.md` design boundaries — "Single static binary, fast startup" and "Deterministic core, LLM only at the edge").
- **Two distinct running instances of the same release:**
  - *Local smith* — the CLI the user runs on **Mac or Windows** (primary), Linux (bonus).
  - *Remote smith* — runs on the **Linux** VPS; bootstrapped by local smith, not typed into directly.
- Because local smith is a Windows/Mac binary, it **cannot copy itself** to a Linux box. Bootstrap = "put the matching Linux build on the box," not "copy myself over."
- The VPS has outbound internet (it's a fresh cloud VPS), so the box can pull its own binary from GitHub Releases.
- Current branch at session start: `feat/vps-setup`. The user opened `.gitignore` in the IDE (may or may not be relevant — GoReleaser's `dist/` output should be gitignored).

## Platform matrix (user-confirmed)
> "this would be for mac and windows users (at least I use both interchangeably) and linux as a bonus. smith should bootstrap itself onto the box"

| GOOS | GOARCH | Who |
|------|--------|-----|
| `darwin` | `arm64` | user, Apple Silicon Mac |
| `darwin` | `amd64` | Intel Mac (optional) |
| `windows` | `amd64` | user, Windows |
| `linux` | `amd64` | VPS + Linux users |
| `linux` | `arm64` | ARM VPSes (Hetzner/Graviton/Ampere) |

## What was discussed
- **Where binaries live:** GitHub **Releases**. Each release ties to a git tag and carries file attachments ("release assets"). Public repos serve them at stable URLs with no auth. Free, generous bandwidth — same mechanism gh/hugo/k9s use. No dedicated site needed.
- **Trigger:** release on **tags, not every push to main**. Keep "green main" (CI tests on push/PR) separate from "published release" (tag → build → upload). Flow: merge to main → `git tag v0.3.0 && git push origin v0.3.0` → workflow runs.
- **Tooling:** **GoReleaser** — cross-compiles all targets from a single Linux runner (no Mac/Windows runners needed for Go), injects version via ldflags, generates changelog from commits, creates the release, uploads assets + `checksums.txt`. Optional extras: Homebrew tap, Docker images, deb/rpm.
- **Bootstrap design (the smith-specific meat):**
  - **Option A (recommended):** local smith SSHes in, VPS pulls its own version-matched Linux asset via `curl` from GitHub Releases. One round-trip, no bytes through the laptop.
  - Option B (rejected as default): local downloads Linux asset to laptop, then `scp` up. More moving parts; only needed for a box with no outbound internet.
  - **Arch detection on the box** via `uname -m` (`x86_64`→amd64, `aarch64`→arm64). Local arch ≠ VPS arch.
  - **Version pinning is mandatory** — install the *exact* version local smith is (`releases/download/v0.3.0/...`, never `/latest/`), so the two lockstep halves never mismatch.
  - **Checksum verification** against GoReleaser's `checksums.txt` before `chmod +x`.
  - Bootstrap should be **idempotent** (skip if `smith version` already matches).
- **Windows notes:** `.exe` suffix handled automatically; Windows 10+ ships OpenSSH client (smith's SSH path works); **code-signing** eventually desirable to avoid SmartScreen warnings (skippable at first).

## Decisions made
- **Distribute via GitHub Releases, no dedicated site** — *firm* — native, free, stable URLs.
- **GoReleaser as the build/release tool** — *firm (recommended, user did not object)* — purpose-built for Go multi-platform releases.
- **Release on tags, not push-to-main** — *firm (recommended)* — separates green-main from published-release.
- **Bootstrap = VPS pulls version-matched Linux asset from GitHub (Option A)** — *working assumption* — cleanest given VPS has internet.
- **Version baked into local smith via ldflags at release time** — *working assumption* — makes "install my exact version on the box" a pure lookup, no config.
- **Platform matrix above** — *firm on Mac(arm64)/Windows(amd64)/Linux(amd64,arm64); Intel Mac optional* — from user's stated usage.

## Draft artifacts (produced this session — review before committing)

### `.goreleaser.yaml`
> Assumes `main` package is at repo root. Adjust `main:` path and the ldflags version-var path (`main.version` etc.) to match smith's actual version-printing code — **verify where smith reads its version before trusting these ldflags.**

```yaml
version: 2

project_name: smith

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - id: smith
    main: .            # TODO: verify main package path
    binary: smith
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - -s -w
      - -X main.version={{ .Version }}
      - -X main.commit={{ .Commit }}
      - -X main.date={{ .CommitDate }}
    goos:
      - darwin
      - windows
      - linux
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64   # drop unless you want Windows ARM
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  # Ship RAW binaries (not tarballs) so the VPS bootstrap can curl a single file
  # at a predictable name. If you'd rather ship tar.gz/zip, change format here and
  # update the bootstrap to download+extract.
  - id: smith
    formats: [binary]
    name_template: "smith_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  use: github
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"

release:
  draft: false
  prerelease: auto     # tags like v0.3.0-rc.1 marked prerelease automatically

# --- Optional, discuss before enabling ---
# brews:
#   - repository:
#       owner: <you>
#       name: homebrew-tap        # separate repo; needs a PAT secret (see workflow)
#     homepage: "https://github.com/<you>/smith"
#     description: "Provisions a VPS and runs the AFK coding-agent loop"
#     install: |
#       bin.install "smith"
```

### `.github/workflows/release.yml`
```yaml
name: release

on:
  push:
    tags: ['v*']

permissions:
  contents: write        # required to create the GitHub Release

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0          # full history for changelog

      - uses: actions/setup-go@v5
        with:
          go-version: stable
          cache: true

      - uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          # Only if a Homebrew tap in a SEPARATE repo is enabled above:
          # HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

### Suggested `.gitignore` addition
```
/dist/
```

### Bootstrap sketch (lives in smith's provisioning code, NOT in CI)
Runs remotely over SSH after arch detection. Pseudocode of the remote command:
```sh
set -euo pipefail
VERSION="v0.3.0"                      # = local smith's own version (ldflags)
case "$(uname -m)" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac
ASSET="smith_${VERSION#v}_linux_${ARCH}"
BASE="https://github.com/<you>/smith/releases/download/${VERSION}"

# idempotency: skip if already the right version
if command -v smith >/dev/null && [ "$(smith version 2>/dev/null)" = "$VERSION" ]; then
  exit 0
fi

curl -fsSL "${BASE}/${ASSET}"      -o /tmp/smith
curl -fsSL "${BASE}/checksums.txt" -o /tmp/checksums.txt
grep " ${ASSET}\$" /tmp/checksums.txt | sed "s# ${ASSET}# /tmp/smith#" | sha256sum -c -
install -m 0755 /tmp/smith /usr/local/bin/smith
```
> Note: the `name_template` in `.goreleaser.yaml` produces `smith_<version>_<os>_<arch>` — keep the bootstrap's `ASSET` string in exact sync with it, including whether the version carries the `v` prefix. Decide on one convention and make both sides match.

## Open questions
- **Homebrew tap?** — proposal: nice `brew install you/tap/smith` UX for a dev audience, but adds a separate `homebrew-tap` repo + a PAT secret to maintain. *(proposal, not committed — start without it.)*
- **Where does smith read its version?** — must confirm the actual Go var path so the ldflags `-X main.version=...` is correct, and so `smith version` output matches the tag format the bootstrap compares against. *(needs code verification.)*
- **Asset naming convention** — with or without `v` prefix, and raw binary vs tar/zip. The draft ships raw binaries named `smith_<version>_<os>_<arch>`; bootstrap must match exactly. *(decide and lock.)*
- **Windows code-signing** — deferred; revisit when non-user Windows people install it. *(proposal, not committed.)*
- **Intel Mac (darwin/amd64)** — included in draft as optional; drop if unwanted.
- **`main:` package path** — draft assumes repo root; verify smith's actual entrypoint location.

## Suggested next steps
1. **Verify smith's version wiring** — find where the version string is defined/printed (`grep -ri "version" cmd/ main.go` or wherever `main` lives). Confirm the ldflags `-X` target path and the exact format `smith version` prints, so it matches the tag.
2. **Confirm the `main:` package path** and adjust `.goreleaser.yaml` `builds[].main` accordingly.
3. **Drop in the two files** (`.goreleaser.yaml`, `.github/workflows/release.yml`) and add `/dist/` to `.gitignore`.
4. **Dry-run locally** — install goreleaser, run `goreleaser release --snapshot --clean`, inspect `dist/` for the 5 expected assets + `checksums.txt`. No tag/push needed for snapshot.
5. **Cut a test tag** (e.g. `v0.0.1-rc.1`) to exercise the real workflow end-to-end; confirm assets land on the Releases page and `prerelease: auto` marks it correctly.
6. **Lock the asset-naming convention**, then write the bootstrap step in smith's provisioning code (arch detect → version-pinned download → checksum verify → idempotent install), keeping the `ASSET` string in sync with `name_template`.
7. **Decide on Homebrew tap** — if yes, create `homebrew-tap` repo, add PAT secret `HOMEBREW_TAP_TOKEN`, uncomment the `brews:` block and the env line in the workflow.

## Key references
- GoReleaser docs: https://goreleaser.com/
- Release asset URL pattern: `https://github.com/<owner>/smith/releases/download/<tag>/<asset>`
- `AGENTS.md` — design boundaries (single static binary; deterministic core).
- `.claude/skills/coding-standards/` — how to write Go in this repo.

## Tried and rejected
- **Release on every push to main** — rejected; would flood Releases with WIP builds. Use tags.
- **Bootstrap by scp-ing a binary from the laptop (Option B)** — rejected as default; local is Windows/Mac so it lacks the Linux binary without first downloading it, adding round-trips. Only revisit for air-gapped/no-outbound-internet boxes.
- **Dedicated website / object storage for downloads** — rejected; GitHub Releases covers it for free.
