# Installing smith

`smith` ships as a single static binary with no runtime dependencies. It runs on
your machine as a control surface, and `machine setup` installs a matching copy
onto each box it provisions ([smith on the box](./on-box.md)).

The current release is **`v0.1.1`**.

> **Fetch with `curl`, not your browser.** smith is unsigned. A browser stamps
> every download with a quarantine flag, so macOS Gatekeeper then refuses to run
> it; `curl` and `wget` don't set that flag, so a curled binary runs untouched.
> The commands below are the supported path for exactly this reason. If you did
> download through a browser, see [Gatekeeper](#gatekeeper-macos).

## Download a release

Each one-liner fetches the `v0.1.1` archive, unpacks it in the current
directory, and leaves the `smith` binary alongside its `LICENSE` and `README`.
Move it onto your `PATH` afterwards (e.g. `sudo mv smith /usr/local/bin/`).

**macOS** (Apple silicon):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_darwin_arm64.tar.gz | tar xz
```

**macOS** (Intel):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_darwin_amd64.tar.gz | tar xz
```

**Linux** (x86-64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_linux_amd64.tar.gz | tar xz
```

**Linux** (ARM64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_linux_arm64.tar.gz | tar xz
```

**Windows** (x86-64) — download the zip, then extract it (modern `tar` on
Windows 10+ handles zips):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_windows_amd64.zip
tar -xf smith_0.1.1_windows_amd64.zip
```

Confirm it: `smith version` should print `0.1.1`.

These one-liners are for the first install, and for every install on Windows. Once smith is on your `PATH`, [`smith update`](#update-smith) moves it to a newer release.

## Update smith

On macOS and Linux, `smith update` replaces your smith with the latest release, then offers to bring every box in your inventory to the same version:

```sh
smith update                     # the latest release
smith update --version v0.2.0    # a named release, even an older one
smith update --boxes             # upgrade every box without asking
```

It downloads the archive for your platform and its `checksums.txt` from GitHub, and aborts on a checksum mismatch, with no retry. The new binary is written beside the old one and renamed over it, so a failure before the rename leaves the old smith working. When `smith` on your `PATH` is a symlink, the file it points to is replaced and the link stays.

smith never escalates to `sudo`. If you cannot write to the directory smith lives in, such as a root-owned `/usr/local/bin`, `smith update` names the directory and changes nothing. Make the directory yours, or reinstall smith into one you own, such as `~/.local/bin`.

Once the local binary is replaced, smith restarts as the new version and lists your boxes in one question:

```text
Upgrade 3 boxes (alpha, beta, dev) to 0.3.0? [y/N]
```

A yes converges each box in turn, the way [`smith machine upgrade`](./on-box.md#machine-upgrade) does. A box that cannot be reached is reported and the rest still go ahead. A no, or a run with no terminal attached and no `--boxes`, upgrades nothing and prints the `smith machine upgrade <box>` command for each box instead. The run ends with one line per box (upgraded, already current, failed or skipped), and exits non-zero if any box failed, even though the local update succeeded.

`smith update` refuses on Windows: download the new release with the one-liner [above](#download-a-release) instead. It also refuses on a box, whose smith version is driven from the operator's machine: run `smith update` there, or `smith machine upgrade <box>`.

## First step: `smith init`

With smith on your `PATH`, scaffold its config home on your machine:

```sh
smith init
smith blueprint check personal
```

`smith init` creates `~/.smith/` and writes two **starters** — a commented
`preferences.yaml` and a blueprint `blueprints/personal.yaml` — filling in your
git identity from your global git config when one is set. Both are valid as
written and resolve to built-in defaults, apart from the git identity init
filled in, until you uncomment something; `smith
blueprint check personal` shows what they resolve to. It never overwrites a file
that already exists, so it is safe to re-run. When you are ready,
`smith machine setup <login>@<host> --blueprint personal` provisions a box from
it — see [Blueprints and preferences](./blueprints.md) for what goes in each
file and [Provisioning a box](./provisioning.md) for setup itself.

## Verify the checksum

The `curl | tar` one-liners are the fast path. To verify first, download the
archive to disk instead of piping it, check it against `checksums.txt`, then
unpack — shown here for macOS Apple silicon (substitute your archive name):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.1/smith_0.1.1_darwin_arm64.tar.gz
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.1/checksums.txt

shasum -a 256 -c checksums.txt --ignore-missing   # macOS
sha256sum   -c checksums.txt --ignore-missing     # Linux

tar xzf smith_0.1.1_darwin_arm64.tar.gz
```

`--ignore-missing` verifies just the archive you downloaded and skips the rest.
Expect a single `... : OK` line.

## Install with `go install`

If you have the Go toolchain (1.26+, matching smith's `go.mod`), build and
install from the module tag directly:

```sh
go install github.com/byranZA/smith/cmd/smith@v0.1.1
```

This resolves the tag, builds from source, and stamps the version from the
module path — `smith version` still reports `0.1.1`, with no ldflags involved.
The binary lands in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`); make sure
that's on your `PATH`.

## Build from source

To build from a checkout instead:

```sh
make build   # produces ./bin/smith
```

A binary built this way reports `smith version` as `dev` — it carries no release
tag. Use `curl` or `go install` for a version-stamped build.

> **A dev build cannot install smith onto a box.** `machine setup` puts a smith
> binary on the box by having it fetch the release matching *your* version, and
> there is no `dev` release — so a from-source build bootstraps a box through
> every phase and then fails at the `install` stage alone. `--smith-version <tag>`
> names a released version to install instead, but that released smith will then
> refuse to relay commands from your dev build, because both sides must match.
> See [smith on the box](./on-box.md#contributing-a-dev-build-cannot-install).

Working on smith itself, rather than just building it? See
[Working on smith](./development.md).

## Gatekeeper (macOS)

If you downloaded through a browser and macOS refuses to open smith — *"smith
cannot be opened because the developer cannot be verified"* — clear the
quarantine flag the browser set, then run it:

```sh
xattr -d com.apple.quarantine ./smith
```

Or just re-fetch with a `curl` one-liner above, which never sets the flag in the
first place.

## Windows / SmartScreen

The Windows story is weaker than the macOS one. SmartScreen keys off
*reputation* as well as Mark-of-the-Web, so a freshly published unsigned binary
can be flagged even when fetched with `curl`, and reputation only builds with
downloads over time. If SmartScreen blocks it, choose **More info → Run anyway**.

## Compatibility

smith is **`v0.x` — there is no compatibility promise.** Flags, config, and
behavior may change between releases without notice. Pin to an exact tag rather
than tracking `latest`.

Local smith and on-box smith must be the **same version**: every relayed command
declares the version it was built by, and on-box smith refuses a mismatch.
`smith machine upgrade <box>` converges a box to your version, and `smith update` offers to converge every box once it has updated your own smith — see [smith on
the box](./on-box.md).
