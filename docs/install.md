# Installing smith

`smith` ships as a single static binary with no runtime dependencies. It runs on
your machine as a control surface, and `machine setup` installs a matching copy
onto each box it provisions ([smith on the box](./on-box.md)).

The current release is **`v0.1.0`**.

> **Fetch with `curl`, not your browser.** smith is unsigned. A browser stamps
> every download with a quarantine flag, so macOS Gatekeeper then refuses to run
> it; `curl` and `wget` don't set that flag, so a curled binary runs untouched.
> The commands below are the supported path for exactly this reason. If you did
> download through a browser, see [Gatekeeper](#gatekeeper-macos).

## Download a release

Each one-liner fetches the `v0.1.0` archive, unpacks it in the current
directory, and leaves the `smith` binary alongside its `LICENSE` and `README`.
Move it onto your `PATH` afterwards (e.g. `sudo mv smith /usr/local/bin/`).

**macOS** (Apple silicon):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_arm64.tar.gz | tar xz
```

**macOS** (Intel):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_amd64.tar.gz | tar xz
```

**Linux** (x86-64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_linux_amd64.tar.gz | tar xz
```

**Linux** (ARM64):

```sh
curl -L https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_linux_arm64.tar.gz | tar xz
```

**Windows** (x86-64) — download the zip, then extract it (modern `tar` on
Windows 10+ handles zips):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_windows_amd64.zip
tar -xf smith_0.1.0_windows_amd64.zip
```

Confirm it: `smith version` should print `0.1.0`.

Newer releases are listed on the [releases
page](https://github.com/byranZA/smith/releases/latest); substitute the tag and
archive name in the commands above.

## Verify the checksum

The `curl | tar` one-liners are the fast path. To verify first, download the
archive to disk instead of piping it, check it against `checksums.txt`, then
unpack — shown here for macOS Apple silicon (substitute your archive name):

```sh
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/smith_0.1.0_darwin_arm64.tar.gz
curl -L -O https://github.com/byranZA/smith/releases/download/v0.1.0/checksums.txt

shasum -a 256 -c checksums.txt --ignore-missing   # macOS
sha256sum   -c checksums.txt --ignore-missing     # Linux

tar xzf smith_0.1.0_darwin_arm64.tar.gz
```

`--ignore-missing` verifies just the archive you downloaded and skips the rest.
Expect a single `... : OK` line.

## Install with `go install`

If you have the Go toolchain (1.26+, matching smith's `go.mod`), build and
install from the module tag directly:

```sh
go install github.com/byranZA/smith/cmd/smith@v0.1.0
```

This resolves the tag, builds from source, and stamps the version from the
module path — `smith version` still reports `0.1.0`, with no ldflags involved.
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
[CONTRIBUTING.md](../CONTRIBUTING.md).

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
`smith machine upgrade <box>` converges a box to your version — see [smith on
the box](./on-box.md).
