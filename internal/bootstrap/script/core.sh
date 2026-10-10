#!/usr/bin/env bash
# bootstrap.sh — smith's on-box provisioning harness.
#
# This is the shipped script. Its source is a set of parts in the bootstrap
# package's script/ directory, one concern each, which the Go side joins in a
# fixed order at package init: this part comes first and main.sh last, and every
# part between declares globals and functions and runs nothing.
#
# The Go side scp's this script to the box and invokes it with a subcommand.
# The version below is load-bearing: once the marker exists it records which
# schema provisioned the box and gates schema skew.
#
# Subcommands:
#   preflight — a non-recorded, read-only gate. It reports the box's privilege
#               level, total memory and raw /etc/os-release so the Go OS support
#               gate can decide and the memory advisory can warn, and mutates
#               nothing.
#   setup     — writes the marker early, then runs the ordered mutating phases.
#               Each phase is check-before-change and appends itself to the
#               marker only on success; progress streams live to the operator.
#               With --remove-dir <dir> it removes <dir>, the directory smith
#               shipped it to, on every exit path; preflight keeps its copy.
#   enroll    — tailscale mode only: joins the box to the tailnet as a tag:smith
#               Tailscale SSH node (auth key on stdin) and prints its tailnet IP.
#   tailscale-status — tailscale mode only: prints the node's tailnet IP when it is
#               already Running, so a re-run skips re-enrollment; mutates nothing.
#   close-public-ssh — tailscale mode only: closes public port 22 and records the
#               access phase, run by smith after a live tailnet probe proves reach.
#
# The marker (/etc/smith/bootstrap.json) is a ledger, not a gate: every setup
# run executes all phases, and check-before-change makes satisfied ones no-ops.
#
# The ordered phases are: swap, packages, tmux-oom-policy, smith-user, smith-keys,
# firewall, ssh-hardening, fail2ban, auto-updates, access. ssh-hardening carries the on-box
# self-reverting self-test (the base layer's only lock-out gate). In public mode
# the access phase is a no-op; in tailscale mode the access layer is driven from
# the admin side (enroll + probe-gated close-public-ssh) rather than as a
# box-side streamed phase.
set -Eeuo pipefail

SMITH_BOOTSTRAP_VERSION=2

# MARKER is the on-box ledger path. SMITH_MARKER overrides it (used by tests to
# point at a writable location without root); production always uses the default.
MARKER="${SMITH_MARKER:-/etc/smith/bootstrap.json}"
MARKER_DIR="$(dirname "$MARKER")"

# MEMINFO is where preflight and probe read the box's total memory, and probe its
# total swap. SMITH_MEMINFO overrides it for tests; production always uses the
# default.
MEMINFO="${SMITH_MEMINFO:-/proc/meminfo}"

# The ordered mutating phases. The names are load-bearing: they are the marker's
# completed_phases values. This is the full base-layer sequence; the access phase
# is where the tailscale mode's behavior lands, not a new phase.
PHASES=(swap packages tmux-oom-policy smith-user smith-keys firewall ssh-hardening fail2ban auto-updates access)

# The smith user smith provisions and the operator lives in thereafter
# (`ssh smith@host`). SMITH_HOME overrides the on-box default for tests, matching
# SMITH_MARKER; production always uses the default.
SMITH_USER="smith"
SMITH_HOME="${SMITH_HOME:-/home/smith}"

# The apt drop-in that enables unattended security upgrades. SMITH_AUTO_UPGRADES_CONF
# overrides it for tests; production always uses the apt.conf.d default.
SMITH_AUTO_UPGRADES_CONF="${SMITH_AUTO_UPGRADES_CONF:-/etc/apt/apt.conf.d/20auto-upgrades}"

# Run parameters, filled by parse_setup_args.
ACCESS="public"
SMITH_VERSION="unknown"
# PUBLIC_SSH is the firewall target for public port 22, derived admin-side by
# smith from the access mode and the door it connected over ("open" keeps 22
# reachable, "closed" removes the allow rule so default-deny drops it). The Go
# side owns the derivation; the firewall phase just obeys it.
PUBLIC_SSH="open"
# BLUEPRINT is the blueprint pointer: the name of the blueprint the box is built
# from, recorded in the marker so a later run can tell what kind of box this is.
# Empty when the box is built from no blueprint. It is a name and nothing else —
# the staged document is ground truth, so no content hash is recorded.
BLUEPRINT=""
# BOX_NAME is the box's name: the operator-chosen name the box is registered
# under, recorded in the marker so the name survives on the box itself and not
# only in the operator's inventory. Empty when the run names the box nothing.
BOX_NAME=""
# REMOVE_DIR is the directory smith shipped this script to, named by setup's
# --remove-dir. The setup run removes it on every exit path, while its bootstrap
# login still reaches the box (hardening closes a root login, so a removal smith
# attempts afterwards is refused). Empty — the default, as when the script is
# run by hand — removes nothing: the script never works the directory out itself.
REMOVE_DIR=""

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

# meminfo_kb prints meminfo's figure for the field named $1 (MemTotal, SwapTotal)
# in kB, or nothing when meminfo cannot be read. It never fails: an unknown
# figure is the Go side's to report.
meminfo_kb() {
  awk -v field="$1:" '$1 == field { print $2; exit }' "$MEMINFO" 2>/dev/null || true
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
  # The name and the blueprint pointer are recorded only when the run has one:
  # a box named nothing, or built from no blueprint, carries no such key at all
  # rather than carrying it empty.
  local optional=""
  if [ -n "$BOX_NAME" ]; then
    optional="${optional}  \"name\": \"${BOX_NAME}\",
"
  fi
  if [ -n "$BLUEPRINT" ]; then
    optional="${optional}  \"blueprint\": \"${BLUEPRINT}\",
"
  fi
  printf '%s\n' "{
  \"schema_version\": ${SMITH_BOOTSTRAP_VERSION},
  \"smith_version\": \"${SMITH_VERSION}\",
  \"access_mode\": \"${ACCESS}\",
${optional}  \"completed_phases\": [$(json_phases)],
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
