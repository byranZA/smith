#!/usr/bin/env bash
# bootstrap.sh — smith's on-box provisioning harness.
#
# The Go side scp's this script to the box and invokes it with a subcommand.
# The version below is load-bearing: once the marker exists it records which
# schema provisioned the box and gates schema skew.
#
# Subcommands:
#   preflight — a non-recorded, read-only gate. It reports the box's privilege
#               level and raw /etc/os-release so the Go OS support gate can
#               decide, and mutates nothing.
#   setup     — writes the marker early, then runs the ordered mutating phases.
#               Each phase is check-before-change and appends itself to the
#               marker only on success; progress streams live to the operator.
#   enroll    — tailscale mode only: joins the box to the tailnet as a tag:smith
#               Tailscale SSH node (auth key on stdin) and prints its tailnet IP.
#   close-public-ssh — tailscale mode only: closes public port 22 and records the
#               access phase, run by smith after a live tailnet probe proves reach.
#
# The marker (/etc/smith/bootstrap.json) is a ledger, not a gate: every setup
# run executes all phases, and check-before-change makes satisfied ones no-ops.
#
# The ordered phases are: packages, smith-user, smith-keys, firewall,
# ssh-hardening, fail2ban, auto-updates, access. ssh-hardening carries the on-box
# self-reverting self-test (the base layer's only lock-out gate). In public mode
# the access phase is a no-op; in tailscale mode the access layer is driven from
# the admin side (enroll + probe-gated close-public-ssh) rather than as a
# box-side streamed phase.
set -Eeuo pipefail

SMITH_BOOTSTRAP_VERSION=1

# MARKER is the on-box ledger path. SMITH_MARKER overrides it (used by tests to
# point at a writable location without root); production always uses the default.
MARKER="${SMITH_MARKER:-/etc/smith/bootstrap.json}"
MARKER_DIR="$(dirname "$MARKER")"

# The ordered mutating phases. The names are load-bearing: they are the marker's
# completed_phases values. This is the full base-layer sequence; the access phase
# is where the tailscale mode's behavior lands, not a new phase.
PHASES=(packages smith-user smith-keys firewall ssh-hardening fail2ban auto-updates access)

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

# The sshd drop-in that hardens SSH (PermitRootLogin no, PasswordAuthentication
# no). SMITH_SSHD_DROPIN overrides it for tests; production always uses the
# sshd_config.d default. The 01- prefix sorts it ahead of distro drop-ins (e.g.
# cloud-init's 50-cloud-init.conf) so sshd's first-match-wins honors the hardened
# values.
SMITH_SSHD_DROPIN="${SMITH_SSHD_DROPIN:-/etc/ssh/sshd_config.d/01-smith-hardening.conf}"

# Run parameters, filled by parse_setup_args.
ACCESS="public"
SMITH_VERSION="unknown"
# PUBLIC_SSH is the firewall target for public port 22, derived admin-side by
# smith from the access mode and the door it connected over ("open" keeps 22
# reachable, "closed" removes the allow rule so default-deny drops it). The Go
# side owns the derivation; the firewall phase just obeys it.
PUBLIC_SSH="open"

# The tailscale apt keyring and sources list. SMITH_TS_KEYRING and SMITH_TS_LIST
# override them for tests; production uses the apt defaults. The keyring is the
# binary .noarmor.gpg blob, so no gnupg is needed to install it.
SMITH_TS_KEYRING="${SMITH_TS_KEYRING:-/usr/share/keyrings/tailscale-archive-keyring.gpg}"
SMITH_TS_LIST="${SMITH_TS_LIST:-/etc/apt/sources.list.d/tailscale.list}"

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

# apt_install runs an idempotent apt-get install of the given packages, streaming
# apt's live output, and reports whether it changed anything in the APT_RESULT
# global ("changed" or "satisfied", read from apt's own newly-installed/upgraded
# counts). The result travels by global rather than stdout so apt's progress
# still streams live to the operator.
APT_RESULT="satisfied"
apt_install() {
  local log
  log="$(mktemp)"
  as_root env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get install -y "$@" 2>&1 | tee "$log"

  local newly upgraded
  newly="$(grep -oE '[0-9]+ newly installed' "$log" | grep -oE '^[0-9]+' | tail -n1 || true)"
  upgraded="$(grep -oE '[0-9]+ upgraded' "$log" | grep -oE '^[0-9]+' | tail -n1 || true)"
  rm -f "$log"

  if [ "${newly:-1}" = "0" ] && [ "${upgraded:-1}" = "0" ]; then
    APT_RESULT="satisfied"
  else
    APT_RESULT="changed"
  fi
}

# ensure_tailscale_repo pins Tailscale's official apt repo for the box's Ubuntu
# codename, using the binary .noarmor.gpg keyring so no gnupg is required. It is
# check-before-change: an already-present keyring and sources list are left as
# they are. It assumes curl and ca-certificates are already installed.
ensure_tailscale_repo() {
  local codename="" base
  if [ -r /etc/os-release ]; then
    codename="$( . /etc/os-release 2>/dev/null; printf '%s' "${VERSION_CODENAME:-}" )"
  fi
  base="https://pkgs.tailscale.com/stable/ubuntu"

  if [ ! -s "$SMITH_TS_KEYRING" ]; then
    as_root mkdir -p -m 0755 "$(dirname "$SMITH_TS_KEYRING")"
    curl -fsSL "${base}/${codename}.noarmor.gpg" | as_root tee "$SMITH_TS_KEYRING" >/dev/null
  fi
  if [ ! -s "$SMITH_TS_LIST" ]; then
    as_root mkdir -p "$(dirname "$SMITH_TS_LIST")"
    curl -fsSL "${base}/${codename}.tailscale-keyring.list" | as_root tee "$SMITH_TS_LIST" >/dev/null
  fi
}

# phase_packages ensures the base package set is installed, plus tailscale (from
# its pinned apt repo) in tailscale mode. It does not probe for presence itself —
# it ensures, it does not assume: an apt-get update then an idempotent apt-get
# install. It is fatal if any apt-get step fails. already-satisfied is read from
# apt's own report of what it changed, so a re-run on a provisioned box is a
# no-op. In tailscale mode curl + ca-certificates are ensured first so the repo
# keyring can be fetched.
phase_packages() {
  as_root env DEBIAN_FRONTEND=noninteractive apt-get update

  local base_pkgs=("${BASE_PACKAGES[@]}")
  if [ "$ACCESS" = "tailscale" ]; then
    base_pkgs+=(curl ca-certificates)
  fi

  local base_result ts_result="satisfied"
  apt_install "${base_pkgs[@]}"
  base_result="$APT_RESULT"

  if [ "$ACCESS" = "tailscale" ]; then
    ensure_tailscale_repo
    as_root env DEBIAN_FRONTEND=noninteractive apt-get update
    apt_install tailscale
    ts_result="$APT_RESULT"
  fi

  if [ "$base_result" = "satisfied" ] && [ "$ts_result" = "satisfied" ]; then
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

# phase_firewall brings up ufw as a default-deny-inbound firewall whose public
# port-22 target is the access-aware PUBLIC_SSH value smith derived: "open" keeps
# an allow-22 rule, "closed" ensures there is none so default-deny drops public
# SSH. This is the seam that makes the firewall access-aware — a tailnet re-run
# (PUBLIC_SSH=closed) never transiently re-opens public 22. It is
# check-before-change: an already-active firewall whose default policy and 22
# rule already match the target is left untouched and reports already-satisfied.
# When the target is open, SSH is allowed before the firewall is enabled so
# activating default-deny never severs the bootstrap connection.
phase_firewall() {
  local want_public_ssh="$PUBLIC_SSH"

  local status
  status="$(as_root ufw status verbose 2>/dev/null || true)"

  local has_ssh_rule=1
  printf '%s\n' "$status" | grep -qE '22/tcp[[:space:]]+ALLOW' || has_ssh_rule=0

  local ssh_ok=1
  if [ "$want_public_ssh" = "open" ] && [ "$has_ssh_rule" -eq 0 ]; then
    ssh_ok=0
  fi
  if [ "$want_public_ssh" = "closed" ] && [ "$has_ssh_rule" -eq 1 ]; then
    ssh_ok=0
  fi

  if printf '%s\n' "$status" | grep -qi 'Status: active' \
    && printf '%s\n' "$status" | grep -qi 'deny (incoming)' \
    && [ "$ssh_ok" -eq 1 ]; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  if [ "$want_public_ssh" = "open" ]; then
    as_root ufw allow 22/tcp
  else
    as_root ufw delete allow 22/tcp >/dev/null 2>&1 || true
  fi
  as_root ufw --force default deny incoming
  as_root ufw --force default allow outgoing
  as_root ufw --force enable
}

# reload_sshd asks systemd to reload the ssh service so a changed sshd config
# takes effect without dropping the connections smith is reaching the box over.
reload_sshd() {
  as_root systemctl reload ssh
}

# ssh_hardening_selftest proves the hardened sshd config is safe before the phase
# trusts it: validate the merged config with `sshd -t`, reload sshd, then
# loopback-probe that smith can still log in (`ssh smith@localhost true`). Any
# step failing returns non-zero so the caller reverts the drop-in.
ssh_hardening_selftest() {
  as_root sshd -t || return 1
  reload_sshd || return 1
  ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
    smith@localhost true || return 1
}

# phase_ssh_hardening applies PermitRootLogin no and PasswordAuthentication no via
# an sshd drop-in — full hardening, no prohibit-password softening and no
# surviving-root backstop. It is the only base-layer phase that closes a door, so
# it never trusts the hardened config blind: it writes the drop-in, then an on-box
# self-test (validate -> reload -> loopback `ssh smith@localhost`) proves smith can
# still log in. On any failure it removes the drop-in, reloads sshd back to the
# working config, and fails the phase without recording success, leaving the box
# reachable as smith — so a mid-sequence failure is never a lock-out. Smith's
# admin-side `ssh smith@host` reconnect is a post-confirm, not this gate. It is
# check-before-change: an already-applied drop-in is a no-op that reports
# already-satisfied and runs no reload or probe.
phase_ssh_hardening() {
  local dropin="$SMITH_SSHD_DROPIN"
  local want='# Managed by smith — hardened sshd. Removing this file reverts the hardening.
PermitRootLogin no
PasswordAuthentication no'

  if [ "$(as_root cat "$dropin" 2>/dev/null || true)" = "$want" ]; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  local dropin_dir tmp
  dropin_dir="$(dirname "$dropin")"
  as_root mkdir -p "$dropin_dir"
  tmp="$(mktemp)"
  printf '%s\n' "$want" >"$tmp"
  as_root install -m 0644 "$tmp" "$dropin"
  rm -f "$tmp"

  if ! ssh_hardening_selftest; then
    as_root rm -f "$dropin"
    reload_sshd || true
    echo "ssh-hardening: self-test failed; reverted the hardening drop-in, box left reachable as smith" >&2
    return 1
  fi
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

# phase_access is the terminal access-layer phase for public mode: the base layer
# already left the box reachable as smith over public SSH, so it changes nothing
# and reports already-satisfied. Tailscale mode does not run this phase — its
# enroll-then-close sequence is gated on an admin-side tailnet probe, so smith
# drives it via the enroll and close-public-ssh subcommands instead. An
# unrecognised mode is a hard error rather than a silent no-op.
phase_access() {
  case "$ACCESS" in
    public)
      PHASE_STATUS="satisfied"
      ;;
    *)
      echo "access: unsupported access mode: ${ACCESS}" >&2
      return 1
      ;;
  esac
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
      --public-ssh)
        PUBLIC_SSH="${2:-}"
        shift 2
        ;;
      *)
        echo "smith bootstrap setup: unknown argument: $1" >&2
        exit 64
        ;;
    esac
  done
}

# setup writes the marker early, then runs each ordered phase. In tailscale mode
# the terminal access phase is not a box-side streamed phase: enrolling the node
# and closing public SSH are gated on a live tailnet probe run from the admin
# side, so smith drives them separately (enroll, then close-public-ssh once the
# probe proves reach) and records the access phase then.
setup() {
  parse_setup_args "$@"
  COMPLETED_PHASES=()
  write_marker
  local phases=("${PHASES[@]}")
  if [ "$ACCESS" = "tailscale" ]; then
    # The terminal access phase is admin-driven in tailscale mode (enroll +
    # probe-gated close-public-ssh), so the box-side sequence is PHASES minus the
    # trailing access element — derived here so the order is defined once, by the
    # PHASES array above, rather than re-listed.
    local last_index=$(( ${#phases[@]} - 1 ))
    if [ "${phases[$last_index]}" != "access" ]; then
      echo "smith bootstrap setup: expected PHASES to end with 'access', found '${phases[$last_index]}'" >&2
      exit 70
    fi
    unset 'phases[last_index]'
  fi
  local name
  for name in "${phases[@]}"; do
    run_phase "$name"
  done
}

# load_marker_state restores the run parameters (access mode, smith version) and
# completed phases from the existing marker, so a follow-up subcommand such as
# close-public-ssh rewrites the marker without clobbering what setup recorded.
load_marker_state() {
  [ -f "$MARKER" ] || return 0
  local am sv
  am="$(grep -o '"access_mode":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  sv="$(grep -o '"smith_version":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  [ -n "$am" ] && ACCESS="$am"
  [ -n "$sv" ] && SMITH_VERSION="$sv"

  COMPLETED_PHASES=()
  local arr item
  arr="$(grep -o '"completed_phases":[[:space:]]*\[[^]]*\]' "$MARKER" || true)"
  arr="${arr#*[}"
  arr="${arr%]*}"
  local IFS=','
  for item in $arr; do
    item="$(printf '%s' "$item" | tr -d ' "')"
    [ -n "$item" ] && COMPLETED_PHASES+=("$item")
  done
}

# enroll joins the box to the tailnet as a tag:smith Tailscale SSH node. The auth
# key is read from stdin (never argv on the smith side), then handed to
# `tailscale up` via its file: scheme so it never lands in the box's process argv
# either — the short-lived tmpfile is 0600 and removed on every exit path.
# Enrollment blocks until the node reaches Running — a missing tag:smith
# tagOwners entry makes `tailscale up` fail here, which is how smith attributes
# that prerequisite. On success it prints the box's tailnet IP for the admin side
# to probe.
enroll() {
  local hostname=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --hostname)
        hostname="${2:-}"
        shift 2
        ;;
      *)
        echo "smith bootstrap enroll: unknown argument: $1" >&2
        exit 64
        ;;
    esac
  done

  local key
  key="$(cat)"
  key="$(printf '%s' "$key" | tr -d '[:space:]')"

  # Hand the key to `tailscale up` via a 0600 tmpfile and its file: scheme rather
  # than --auth-key=<literal>, so the secret never appears in the box's process
  # argv (ps / /proc/PID/cmdline). The trap removes it on any exit path.
  local keyfile
  keyfile="$(mktemp)"
  chmod 600 "$keyfile"
  # shellcheck disable=SC2064
  trap "rm -f '$keyfile'" EXIT
  printf '%s' "$key" >"$keyfile"

  as_root tailscale up --auth-key="file:$keyfile" --hostname="$hostname" --advertise-tags=tag:smith --ssh

  rm -f "$keyfile"
  trap - EXIT

  local ip
  ip="$(as_root tailscale ip -4 2>/dev/null | head -n1 || true)"
  if [ -z "$ip" ]; then
    echo "enroll: node did not reach Running with a tailnet IP" >&2
    exit 1
  fi
  printf 'tailscale-ip=%s\n' "$ip"
}

# close_public_ssh removes the public port-22 allow rule so default-deny drops
# public SSH — the tailscale access layer's last mutating step, run by smith only
# after the live tailnet probe proves the new door opens. It then records the
# access phase in the marker, completing the tailscale phase set.
close_public_ssh() {
  as_root ufw delete allow 22/tcp >/dev/null 2>&1 || true
  load_marker_state
  mark_complete access
}

# smith_has_sudo reports whether the invoking user can run privileged commands
# without a password: true as root, or with passwordless sudo. Read-only — it is
# what the status probe uses to decide whether the root-only facts are
# determinable at all.
smith_has_sudo() {
  if [ "$(id -u)" -eq 0 ]; then
    return 0
  fi
  sudo -n true >/dev/null 2>&1
}

# probe_firewall emits the ufw facts (active, default-deny inbound, whether public
# port 22 is allowed). An unreadable status yields '?' for all three.
probe_firewall() {
  local status
  status="$(as_root ufw status verbose 2>/dev/null || true)"
  if [ -z "$status" ]; then
    echo "ufw-active=?"
    echo "ufw-default-deny=?"
    echo "ufw-ssh-allow=?"
    return 0
  fi
  if printf '%s\n' "$status" | grep -qi 'Status: active'; then
    echo "ufw-active=active"
  else
    echo "ufw-active=inactive"
  fi
  if printf '%s\n' "$status" | grep -qi 'deny (incoming)'; then
    echo "ufw-default-deny=deny"
  else
    echo "ufw-default-deny=allow"
  fi
  if printf '%s\n' "$status" | grep -qE '22/tcp[[:space:]]+ALLOW'; then
    echo "ufw-ssh-allow=allow"
  else
    echo "ufw-ssh-allow=deny"
  fi
}

# probe_sshd emits sshd's effective PermitRootLogin and PasswordAuthentication,
# read from `sshd -T` (the merged effective config) rather than by grepping a
# file. A value that cannot be read is emitted as '?'.
probe_sshd() {
  local cfg
  cfg="$(as_root sshd -T 2>/dev/null || true)"
  if [ -z "$cfg" ]; then
    echo "permit-root-login=?"
    echo "password-authentication=?"
    return 0
  fi
  local prl pa
  prl="$(printf '%s\n' "$cfg" | grep -i '^permitrootlogin ' | awk '{print $2}' | head -n1)"
  pa="$(printf '%s\n' "$cfg" | grep -i '^passwordauthentication ' | awk '{print $2}' | head -n1)"
  echo "permit-root-login=${prl:-?}"
  echo "password-authentication=${pa:-?}"
}

# probe_services emits whether fail2ban is running and whether unattended
# security upgrades are enabled.
probe_services() {
  if as_root systemctl is-active --quiet fail2ban 2>/dev/null; then
    echo "fail2ban=running"
  else
    echo "fail2ban=stopped"
  fi

  local conf
  conf="$(as_root cat "$SMITH_AUTO_UPGRADES_CONF" 2>/dev/null || true)"
  if printf '%s' "$conf" | grep -q 'APT::Periodic::Unattended-Upgrade "1"'; then
    echo "auto-updates=enabled"
  else
    echo "auto-updates=disabled"
  fi
}

# probe_access emits the tailscale-mode access facts smith can read on the box:
# the Tailscale backend state and the node's tailnet IP (used by the admin side
# to run the live ssh-over-tailnet probe). Neither needs root. In public mode the
# tailscale binary is absent, so this reports '?' and the reconciler ignores it.
probe_access() {
  local state ip
  state="$(tailscale status --json 2>/dev/null | grep -o '"BackendState":[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' | head -n1 || true)"
  if [ "$state" = "Running" ]; then
    echo "tailscale-running=running"
  elif [ -n "$state" ]; then
    echo "tailscale-running=stopped"
  else
    echo "tailscale-running=?"
  fi
  ip="$(tailscale ip -4 2>/dev/null | head -n1 || true)"
  if [ -n "$ip" ]; then
    echo "tailnet-ip=${ip}"
  fi
}

# probe emits the box's live, read-only facts for the drift reconciler: the
# marker (in a delimited block), whether the smith user exists and still holds
# passwordless sudo, and — only when sudo is available — the firewall, sshd,
# fail2ban, and auto-updates facts. When passwordless sudo is lost, the
# root-only facts are emitted as '?' so the reconciler marks them unverifiable.
# It is query-only: it mutates nothing.
probe() {
  echo "marker-begin"
  cat "$MARKER" 2>/dev/null || true
  echo "marker-end"

  if getent passwd "$SMITH_USER" >/dev/null 2>&1; then
    echo "smith-user-exists=yes"
  else
    echo "smith-user-exists=no"
  fi

  if smith_has_sudo; then
    echo "passwordless-sudo=yes"
    probe_firewall
    probe_sshd
    probe_services
  else
    echo "passwordless-sudo=no"
    echo "ufw-active=?"
    echo "ufw-default-deny=?"
    echo "ufw-ssh-allow=?"
    echo "permit-root-login=?"
    echo "password-authentication=?"
    echo "fail2ban=?"
    echo "auto-updates=?"
  fi

  probe_access
}

main() {
  local sub="${1:-}"
  case "$sub" in
    preflight)
      preflight
      ;;
    probe)
      probe
      ;;
    setup)
      shift
      setup "$@"
      ;;
    enroll)
      shift
      enroll "$@"
      ;;
    close-public-ssh)
      close_public_ssh
      ;;
    *)
      echo "smith bootstrap: unknown subcommand: ${sub:-<none>}" >&2
      exit 64
      ;;
  esac
}

main "$@"
