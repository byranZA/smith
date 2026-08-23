#!/usr/bin/env bash
# install.sh — the box side of smith's install stage.
#
# The Go side (the onbox package) scp's this script to the box and invokes it
# with a subcommand. It is deliberately not part of bootstrap.sh: that script is
# the base layer that provisions any box, and how smith distributes itself does
# not belong in it.
#
# Subcommands:
#   probe   — read-only: prints the box's machine hardware name and the smith
#             version already installed, if any. Mutates nothing, so a box
#             already at the right version never reaches install.
#   install — downloads the named release archive and the release's published
#             checksums to a temp directory, verifies the archive against them,
#             and only then installs it root-owned and 0755.
#
# Verification is not optional: a mismatch is a hard abort with no retry and no
# install-anyway fallback. Everything before the final install(1) happens under
# a temp directory, so a failed download or a bad checksum leaves whatever smith
# the box already had exactly where it was — a working box at its old version is
# the state worth having when a release cannot be fetched mid-run.
#
# It writes nothing but the binary: no marker field records what is installed,
# because `smith version` on the box is ground truth and a stored copy could
# drift.
set -Eeuo pipefail

# SMITH_INSTALL_PATH is where smith lives on the box. The override exists for
# tests, matching bootstrap.sh's own SMITH_* overrides; production always uses
# the default, which the Go side's onbox.InstallPath spells the same way.
SMITH_INSTALL_PATH="${SMITH_INSTALL_PATH:-/usr/local/bin/smith}"

# WORKDIR is the temp directory the archive and the checksums are downloaded
# into. It is a global rather than a local so the EXIT trap can remove it
# however the run ends, which is what keeps an unverified download off the box.
WORKDIR=""

# cleanup removes the download directory, so nothing but a verified binary
# outlives the run.
cleanup() {
  if [ -n "$WORKDIR" ]; then
    rm -rf "$WORKDIR"
  fi
}

# as_root runs a command with privilege: directly when already root, else through
# passwordless sudo (which the smith user has, and which bootstrap established).
as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo -n "$@"
  fi
}

# probe reports what the Go side needs before it decides anything: the machine
# hardware name to pick a release asset by, and the smith version already
# installed. A box with no smith prints no version line at all, which is how
# "never installed" is told from "installed and reporting".
probe() {
  printf 'arch=%s\n' "$(uname -m)"
  if [ -x "$SMITH_INSTALL_PATH" ]; then
    local reported
    reported="$("$SMITH_INSTALL_PATH" version 2>/dev/null | head -n 1 | awk '{print $2}')" || reported=""
    if [ -n "$reported" ]; then
      printf 'smith-version=%s\n' "$reported"
    fi
  fi
}

# install_smith downloads, verifies and installs one release archive. The
# archive and the checksums land in a temp directory that is removed however the
# run ends, so nothing but a verified binary ever reaches the install path.
install_smith() {
  local url="" checksums_url=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --url) url="$2"; shift 2 ;;
      --checksums-url) checksums_url="$2"; shift 2 ;;
      *) echo "install: unknown argument: $1" >&2; exit 2 ;;
    esac
  done
  if [ -z "$url" ] || [ -z "$checksums_url" ]; then
    echo "install: --url and --checksums-url are both required" >&2
    exit 2
  fi

  local archive
  WORKDIR="$(mktemp -d)"
  trap cleanup EXIT
  archive="$WORKDIR/${url##*/}"

  # No --retry: a download that failed is reported rather than repeated, so a
  # release that is unreachable or serving something unexpected mid-run leaves
  # the box at the version it already had.
  echo "downloading $url"
  curl -fsSL -o "$archive" "$url"
  curl -fsSL -o "$WORKDIR/checksums.txt" "$checksums_url"

  # The published checksums cover every asset of the release, so only the one
  # that was downloaded is checked. A mismatch aborts here, before anything is
  # extracted and long before the binary is replaced.
  echo "verifying ${archive##*/} against the published checksums"
  if ! (cd "$WORKDIR" && sha256sum --ignore-missing --check checksums.txt >/dev/null 2>&1); then
    echo "checksum mismatch: ${archive##*/} does not match the published checksums; nothing was installed" >&2
    exit 1
  fi

  tar -xzf "$archive" -C "$WORKDIR" smith
  as_root install -m 0755 -o root -g root "$WORKDIR/smith" "$SMITH_INSTALL_PATH"
  echo "installed $SMITH_INSTALL_PATH"
}

main() {
  local subcommand="${1:-}"
  shift || true
  case "$subcommand" in
    probe) probe ;;
    install) install_smith "$@" ;;
    *) echo "usage: install.sh {probe|install --url <url> --checksums-url <url>}" >&2; exit 2 ;;
  esac
}

main "$@"
