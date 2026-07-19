#!/usr/bin/env bash
# bootstrap.sh — smith's on-box provisioning harness.
#
# The Go side scp's this script to the box and invokes it with a subcommand.
# The version below is load-bearing: once the marker exists it records which
# script provisioned the box and gates schema skew.
#
# For issue #11 the only implemented subcommand is the non-recorded `preflight`
# gate. It mutates NOTHING: it reports the box's privilege level and raw
# /etc/os-release so the Go OS support gate can decide, then exits. The ordered
# mutating phases (packages, smith-user, ...) land in later issues.
set -Eeuo pipefail

SMITH_BOOTSTRAP_VERSION=1

# detect_privilege reports how smith can run privileged commands on this box:
#   root  — the bootstrap login is root, no sudo needed
#   sudo  — a non-root login with passwordless sudo
#   none  — a non-root login without passwordless sudo (setup cannot proceed)
detect_privilege() {
  if [ "$(id -u)" -eq 0 ]; then
    echo "root"
  elif sudo -n true >/dev/null 2>&1; then
    echo "sudo"
  else
    echo "none"
  fi
}

# preflight emits the facts the Go side needs to gate the box. It is a
# read-only probe: it changes nothing.
preflight() {
  echo "smith-preflight version=${SMITH_BOOTSTRAP_VERSION}"
  echo "privilege=$(detect_privilege)"
  echo "os-release-begin"
  cat /etc/os-release
  echo "os-release-end"
}

main() {
  case "${1:-}" in
    preflight) preflight ;;
    *)
      echo "smith bootstrap: unknown subcommand: ${1:-<none>}" >&2
      exit 64
      ;;
  esac
}

main "$@"
