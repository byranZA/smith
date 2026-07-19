#!/usr/bin/env bash
# bootstrap.sh — smith's on-box provisioning harness.
#
# The Go side scp's this script to the box and invokes it with a subcommand.
# The version below is load-bearing: once the marker exists it records which
# schema provisioned the box and gates schema skew.
#
# Two subcommands are implemented:
#   preflight — a non-recorded, read-only gate. It reports the box's privilege
#               level and raw /etc/os-release so the Go OS support gate can
#               decide, and mutates nothing.
#   setup     — writes the marker early, then runs the ordered mutating phases.
#               Each phase is check-before-change and appends itself to the
#               marker only on success; progress streams live to the operator.
#
# The marker (/etc/smith/bootstrap.json) is a ledger, not a gate: every setup
# run executes all phases, and check-before-change makes satisfied ones no-ops.
#
# Only the `packages` phase is implemented so far; the remaining ordered phases
# (smith-user, smith-keys, firewall, ssh-hardening, ...) land in later issues.
set -Eeuo pipefail

SMITH_BOOTSTRAP_VERSION=1

# MARKER is the on-box ledger path. SMITH_MARKER overrides it (used by tests to
# point at a writable location without root); production always uses the default.
MARKER="${SMITH_MARKER:-/etc/smith/bootstrap.json}"
MARKER_DIR="$(dirname "$MARKER")"

# The ordered mutating phases. The names are load-bearing: they are the marker's
# completed_phases values. Later issues extend this list.
PHASES=(packages)

# The base package set every box gets, regardless of access mode.
BASE_PACKAGES=(fail2ban ufw unattended-upgrades)

# Run parameters, filled by parse_setup_args.
ACCESS="public"
SMITH_VERSION="unknown"

# COMPLETED_PHASES accumulates the phases that have succeeded this run; it is
# rendered into the marker after each one. PHASE_STATUS carries a phase's
# check-before-change result ("changed" or "satisfied") back to the loop.
COMPLETED_PHASES=()
PHASE_STATUS="changed"

# as_root runs its arguments with root privilege: directly when the bootstrap
# login is already root, otherwise via passwordless sudo.
as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo -n "$@"
  fi
}

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

# json_phases renders COMPLETED_PHASES as a JSON array body (the phase names,
# quoted and comma-separated). Empty when no phase has completed yet.
json_phases() {
  local body="" sep="" p
  for p in ${COMPLETED_PHASES[@]+"${COMPLETED_PHASES[@]}"}; do
    body="${body}${sep}\"${p}\""
    sep=", "
  done
  printf '%s' "$body"
}

# write_marker renders the marker from the current run state and writes it
# atomically as root. It is called early (empty completed_phases) and again
# after each phase succeeds.
write_marker() {
  local ts
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  as_root mkdir -p "$MARKER_DIR"
  printf '%s\n' "{
  \"schema_version\": ${SMITH_BOOTSTRAP_VERSION},
  \"smith_version\": \"${SMITH_VERSION}\",
  \"access_mode\": \"${ACCESS}\",
  \"completed_phases\": [$(json_phases)],
  \"updated_at\": \"${ts}\"
}" | as_root tee "$MARKER" >/dev/null
}

# mark_complete records a phase as completed (idempotently) and rewrites the
# marker, so completed_phases is appended to only on a phase's success.
mark_complete() {
  local name="$1" p
  for p in ${COMPLETED_PHASES[@]+"${COMPLETED_PHASES[@]}"}; do
    if [ "$p" = "$name" ]; then
      write_marker
      return 0
    fi
  done
  COMPLETED_PHASES+=("$name")
  write_marker
}

# run_phase runs one phase, streaming its live progress and a final success or
# already-satisfied marker, then records it. A phase that fails aborts setup via
# set -e before the success marker is printed or recorded.
run_phase() {
  local name="$1"
  printf '▶ %s\n' "$name"
  PHASE_STATUS="changed"
  "phase_${name}"
  if [ "$PHASE_STATUS" = "satisfied" ]; then
    printf '✓ %s (already-satisfied)\n' "$name"
  else
    printf '✓ %s\n' "$name"
  fi
  mark_complete "$name"
}

# phase_packages ensures the base package set is installed. It does not probe
# for presence itself — it ensures, it does not assume: a single apt-get update
# then an idempotent apt-get install. It is fatal if either apt-get step fails.
# already-satisfied is read from apt's own report of what it changed, so a
# re-run on a provisioned box is a no-op.
phase_packages() {
  as_root env DEBIAN_FRONTEND=noninteractive apt-get update

  local log
  log="$(mktemp)"
  as_root env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y "${BASE_PACKAGES[@]}" 2>&1 | tee "$log"

  local newly upgraded
  newly="$(grep -oE '[0-9]+ newly installed' "$log" | grep -oE '^[0-9]+' | tail -n1 || true)"
  upgraded="$(grep -oE '[0-9]+ upgraded' "$log" | grep -oE '^[0-9]+' | tail -n1 || true)"
  rm -f "$log"

  if [ "${newly:-1}" = "0" ] && [ "${upgraded:-1}" = "0" ]; then
    PHASE_STATUS="satisfied"
  else
    PHASE_STATUS="changed"
  fi
}

# parse_setup_args reads the setup subcommand's flags: the access mode and the
# smith version to stamp into the marker.
parse_setup_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --access)
        ACCESS="${2:-}"
        shift 2
        ;;
      --smith-version)
        SMITH_VERSION="${2:-}"
        shift 2
        ;;
      *)
        echo "smith bootstrap setup: unknown argument: $1" >&2
        exit 64
        ;;
    esac
  done
}

# setup writes the marker early, then runs each ordered phase.
setup() {
  parse_setup_args "$@"
  COMPLETED_PHASES=()
  write_marker
  local name
  for name in "${PHASES[@]}"; do
    run_phase "$name"
  done
}

main() {
  local sub="${1:-}"
  case "$sub" in
    preflight)
      preflight
      ;;
    setup)
      shift
      setup "$@"
      ;;
    *)
      echo "smith bootstrap: unknown subcommand: ${sub:-<none>}" >&2
      exit 64
      ;;
  esac
}

main "$@"
