# apt: the base package set, the first-boot apt-lock wait every apt-get call
# carries, and the Tailscale apt repo.

# The base package set every box gets, regardless of access mode.
BASE_PACKAGES=(fail2ban ufw unattended-upgrades)

# APT_LOCK_TIMEOUT is the single hardcoded budget (seconds) for waiting out a busy
# apt lock. On a freshly booted cloud image, cloud-init / unattended-upgrades holds
# the apt locks for the first minute or two; without a wait, the packages phase
# races that and aborts setup at the highest-friction moment (a new user's very
# first `smith machine setup`). 180s comfortably absorbs a first-boot run while
# still failing eventually on a genuinely stuck lock. It is deliberately not
# configurable: smith has no local config file in v1 (docs/adr/0003).
APT_LOCK_TIMEOUT=180
# APT_LOCK_OPTS carries apt's own lock-wait to every apt-get call. DPkg::Lock::Timeout
# reliably serializes against the dpkg/frontend locks (covering install); the
# /var/lib/apt/lists/lock that `apt-get update` takes is not guaranteed to honor it
# on every apt version, so update additionally rides a bounded retry loop (apt_update).
APT_LOCK_OPTS=(-o "DPkg::Lock::Timeout=${APT_LOCK_TIMEOUT}")

# The tailscale apt keyring and sources list. SMITH_TS_KEYRING and SMITH_TS_LIST
# override them for tests; production uses the apt defaults. The keyring is the
# binary .noarmor.gpg blob, so no gnupg is needed to install it.
SMITH_TS_KEYRING="${SMITH_TS_KEYRING:-/usr/share/keyrings/tailscale-archive-keyring.gpg}"
SMITH_TS_LIST="${SMITH_TS_LIST:-/etc/apt/sources.list.d/tailscale.list}"

# apt_update runs `apt-get update` with a lock-wait. It carries apt's own
# DPkg::Lock::Timeout, but because the /var/lib/apt/lists/lock update takes is not
# guaranteed to honor that option on every apt version — and that lists lock is
# exactly the first-boot race — it also retries within a bounded APT_LOCK_TIMEOUT
# budget: a lock-held failure is retried until the box's own apt finishes, and a
# genuinely stuck lock still fails once the budget is exhausted (returning non-zero
# so set -e aborts, preserving the fail-loud contract). The first time a call is
# actually blocked, it streams a progress line so a paused phase does not look hung.
apt_update() {
  local deadline=$(( SECONDS + APT_LOCK_TIMEOUT )) announced=0
  while :; do
    if as_root env DEBIAN_FRONTEND=noninteractive apt-get "${APT_LOCK_OPTS[@]}" update; then
      return 0
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      return 1
    fi
    if [ "$announced" -eq 0 ]; then
      printf '  waiting for apt lock…\n'
      announced=1
    fi
    sleep 3
  done
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
  as_root env DEBIAN_FRONTEND=noninteractive LC_ALL=C apt-get "${APT_LOCK_OPTS[@]}" install -y "$@" 2>&1 | tee "$log"

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
