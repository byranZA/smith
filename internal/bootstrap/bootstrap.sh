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
# The `packages`, `smith-user`, `smith-keys`, `firewall`, `fail2ban`, and
# `auto-updates` phases are implemented so far; the remaining ordered phases
# (ssh-hardening, access) land in later issues.
set -Eeuo pipefail

SMITH_BOOTSTRAP_VERSION=1

# MARKER is the on-box ledger path. SMITH_MARKER overrides it (used by tests to
# point at a writable location without root); production always uses the default.
MARKER="${SMITH_MARKER:-/etc/smith/bootstrap.json}"
MARKER_DIR="$(dirname "$MARKER")"

# The ordered mutating phases. The names are load-bearing: they are the marker's
# completed_phases values. Later issues extend this list.
PHASES=(packages smith-user smith-keys firewall fail2ban auto-updates)

# The base package set every box gets, regardless of access mode.
BASE_PACKAGES=(fail2ban ufw unattended-upgrades)

# The smith user smith provisions and the operator lives in thereafter
# (`ssh smith@host`). SMITH_HOME and SMITH_SUDOERS_DIR override the on-box
# defaults for tests, matching SMITH_MARKER; production always uses the defaults.
SMITH_USER="smith"
SMITH_HOME="${SMITH_HOME:-/home/smith}"
SMITH_SUDOERS_DIR="${SMITH_SUDOERS_DIR:-/etc/sudoers.d}"

# The apt drop-in that enables unattended security upgrades. SMITH_AUTO_UPGRADES_CONF
# overrides it for tests; production always uses the apt.conf.d default.
SMITH_AUTO_UPGRADES_CONF="${SMITH_AUTO_UPGRADES_CONF:-/etc/apt/apt.conf.d/20auto-upgrades}"

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
  # Phase names are hyphenated (they are the marker's values); the functions that
  # implement them use underscores, since bash function names cannot contain '-'.
  "phase_${name//-/_}"
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

# phase_smith_user creates the smith user with its home directory and grants it
# passwordless sudo via a validated /etc/sudoers.d drop-in. It is
# check-before-change: an existing user and an already-correct drop-in are left
# untouched, so a re-run is a no-op that reports already-satisfied.
phase_smith_user() {
  local changed=0

  # Create the user (with its home) only if it does not already exist.
  if ! getent passwd "$SMITH_USER" >/dev/null 2>&1; then
    as_root useradd --create-home --home-dir "$SMITH_HOME" --shell /bin/bash "$SMITH_USER"
    changed=1
  fi

  # Ensure the passwordless-sudo drop-in matches the expected grant, validating
  # it with visudo before it lands so a malformed file never reaches sudoers.d.
  local sudoers_file="${SMITH_SUDOERS_DIR}/smith"
  local sudoers_line="smith ALL=(ALL) NOPASSWD:ALL"
  if [ "$(as_root cat "$sudoers_file" 2>/dev/null || true)" != "$sudoers_line" ]; then
    local tmp
    tmp="$(mktemp)"
    printf '%s\n' "$sudoers_line" >"$tmp"
    as_root visudo -cf "$tmp"
    as_root mkdir -p "$SMITH_SUDOERS_DIR"
    as_root install -m 0440 "$tmp" "$sudoers_file"
    rm -f "$tmp"
    changed=1
  fi

  if [ "$changed" -eq 0 ]; then
    PHASE_STATUS="satisfied"
  fi
}

# phase_smith_keys copies the bootstrap login's authorized_keys onto the smith
# user — smith reuses the operator's existing SSH identity rather than minting a
# CA of its own. It is check-before-change: if smith already carries exactly the
# bootstrap login's keys, the phase is a no-op that reports already-satisfied.
phase_smith_keys() {
  local src="${HOME}/.ssh/authorized_keys"
  local dest_dir="${SMITH_HOME}/.ssh"
  local dest="${dest_dir}/authorized_keys"

  if [ ! -f "$src" ]; then
    echo "smith-keys: bootstrap login has no authorized_keys at ${src}" >&2
    return 1
  fi

  if [ "$(as_root cat "$dest" 2>/dev/null || true)" = "$(cat "$src")" ]; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  as_root mkdir -p "$dest_dir"
  as_root cp "$src" "$dest"
  as_root chmod 0700 "$dest_dir"
  as_root chmod 0600 "$dest"
  as_root chown -R "${SMITH_USER}:${SMITH_USER}" "$dest_dir"
}

# public_ssh_target reports whether public SSH (port 22) should stay open,
# derived from the door smith connected over. Only public mode exists in this
# slice, so port 22 stays open; the tailscale access mode (#18) slots its branch
# in here to keep public 22 closed once tailnet reach is proven. This is the
# seam that makes the firewall phase access-aware.
public_ssh_target() {
  case "$ACCESS" in
    *) echo "open" ;;
  esac
}

# phase_firewall brings up ufw as a default-deny-inbound firewall, allowing SSH
# on port 22 when the connect door is public. It is check-before-change: an
# already-active firewall with the expected default policy and SSH rule is left
# untouched and reports already-satisfied. SSH is allowed before the firewall is
# enabled so activating default-deny never severs the bootstrap connection.
phase_firewall() {
  local want_public_ssh
  want_public_ssh="$(public_ssh_target)"

  local status
  status="$(as_root ufw status verbose 2>/dev/null || true)"

  local ssh_ok=1
  if [ "$want_public_ssh" = "open" ]; then
    printf '%s\n' "$status" | grep -qE '22/tcp[[:space:]]+ALLOW' || ssh_ok=0
  fi

  if printf '%s\n' "$status" | grep -qi 'Status: active' \
    && printf '%s\n' "$status" | grep -qi 'deny (incoming)' \
    && [ "$ssh_ok" -eq 1 ]; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  if [ "$want_public_ssh" = "open" ]; then
    as_root ufw allow 22/tcp
  fi
  as_root ufw --force default deny incoming
  as_root ufw --force default allow outgoing
  as_root ufw --force enable
}

# phase_fail2ban ensures the fail2ban service (installed by the packages phase)
# is enabled and running. It is check-before-change: an already active+enabled
# service is left untouched and reports already-satisfied.
phase_fail2ban() {
  if as_root systemctl is-active --quiet fail2ban \
    && as_root systemctl is-enabled --quiet fail2ban; then
    PHASE_STATUS="satisfied"
    return 0
  fi
  as_root systemctl enable --now fail2ban
}

# phase_auto_updates enables unattended *security* upgrades via the apt periodic
# drop-in, then triggers one unattended-upgrades run so the box is patched at
# handoff — no blanket apt upgrade. It is check-before-change: when the drop-in
# already matches, the phase is a no-op that reports already-satisfied and does
# not re-run the patch (the box was patched on the run that enabled it).
phase_auto_updates() {
  local want='APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";'

  if [ "$(as_root cat "$SMITH_AUTO_UPGRADES_CONF" 2>/dev/null || true)" = "$want" ]; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  local tmp
  tmp="$(mktemp)"
  printf '%s\n' "$want" >"$tmp"
  as_root install -m 0644 "$tmp" "$SMITH_AUTO_UPGRADES_CONF"
  rm -f "$tmp"

  # One-time patch at handoff. unattended-upgrade only pulls the configured
  # (security) origins, so this is not a blanket apt upgrade.
  as_root unattended-upgrade
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
