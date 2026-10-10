# phases-system: the phases that shape the box itself: swap, packages,
# tmux-oom-policy, fail2ban and auto-updates.

# The swap phase's system paths: the kernel's active swap list, the fstab that
# brings swap back after a reboot, and the swapfile smith creates. SMITH_SWAPS,
# SMITH_FSTAB and SMITH_SWAPFILE override them for tests; production always uses
# the defaults.
SWAPS="${SMITH_SWAPS:-/proc/swaps}"
FSTAB="${SMITH_FSTAB:-/etc/fstab}"
SWAPFILE="${SMITH_SWAPFILE:-/swapfile}"

# The swapfile is the smaller of SWAP_MAX_MB and a quarter of the free disk on its
# filesystem; under SWAP_MIN_MB it is too small to be worth having, so none is made.
SWAP_MAX_MB=2048
SWAP_MIN_MB=256

# The tmux-oom-policy phase's drop-in for tmux's pane scopes. tmux 3.4 runs each
# pane in a transient tmux-spawn-<id>.scope of the user's systemd manager, so the
# drop-in lives in the user-unit tree; its tmux-spawn- prefix makes it apply to
# those scopes alone. SMITH_TMUX_SCOPE_DROPIN overrides it for tests; production
# always uses the default.
SMITH_TMUX_SCOPE_DROPIN="${SMITH_TMUX_SCOPE_DROPIN:-/etc/systemd/user/tmux-spawn-.scope.d/50-smith-oom-policy.conf}"
# SCOPE_OOM_POLICY_MIN_SYSTEMD is the first systemd release that honours OOMPolicy=
# on a scope unit; an older one ignores the drop-in.
SCOPE_OOM_POLICY_MIN_SYSTEMD=253
# TMUX_OOM_RELOAD_PENDING stamps a tmux-oom-policy run whose drop-in may be in
# place before every user manager has reloaded it; while it exists, a matching
# drop-in does not make the phase satisfied.
TMUX_OOM_RELOAD_PENDING="${MARKER_DIR}/tmux-oom-policy.reload-pending"

# swap_active reports whether the box has any active swap: the kernel's swap list
# carries an entry below its header line.
swap_active() {
  [ "$(tail -n +2 "$SWAPS" 2>/dev/null | grep -c .)" -gt 0 ]
}

# swapfile_active reports whether smith's swapfile is in the kernel's swap list.
swapfile_active() {
  tail -n +2 "$SWAPS" 2>/dev/null | awk -v f="$SWAPFILE" '$1 == f { found = 1 } END { exit !found }'
}

# swap_fstab_entry_present reports whether fstab carries a reboot entry for the
# swapfile.
swap_fstab_entry_present() {
  as_root awk -v f="$SWAPFILE" '$1 == f { found = 1 } END { exit !found }' "$FSTAB" 2>/dev/null
}

# ensure_swap_fstab_entry adds the swapfile's reboot entry to fstab, unless an
# entry for the swapfile is already there.
ensure_swap_fstab_entry() {
  if swap_fstab_entry_present; then
    return 0
  fi
  printf '%s none swap sw 0 0\n' "$SWAPFILE" | as_root tee -a "$FSTAB" >/dev/null
}

# remove_swap_fstab_entry drops any reboot entry for the swapfile from fstab.
remove_swap_fstab_entry() {
  local kept
  [ -f "$FSTAB" ] || return 0
  kept="$(as_root awk -v f="$SWAPFILE" '$1 != f' "$FSTAB")"
  printf '%s' "${kept:+$kept$'\n'}" | as_root tee "$FSTAB" >/dev/null
}

# swap_size_mb prints the swapfile's size in MB: the smaller of SWAP_MAX_MB and a
# quarter of the free disk on the swapfile's filesystem.
swap_size_mb() {
  local free_kb size
  free_kb="$(df -Pk "$(dirname "$SWAPFILE")" | awk 'NR == 2 { print $4 }')"
  size=$(( free_kb / 1024 / 4 ))
  if [ "$size" -gt "$SWAP_MAX_MB" ]; then
    size="$SWAP_MAX_MB"
  fi
  printf '%s\n' "$size"
}

# phase_swap gives a box without swap a root-only swapfile, formatted, enabled and
# set to come back after a reboot, so memory pressure slows the box rather than
# freezing it. It adapts to the box rather than failing setup: any active swap,
# smith's or not, makes it a no-op that reports already-satisfied; too little disk
# skips it; and a box that refuses swap gets the partial swapfile and its reboot
# entry removed, then skips it. Every skip prints a note line. The reboot entry is
# written before the swapfile is enabled, so a failed write leaves no active swap
# and a retry runs the phase again; an active swapfile of smith's own that has lost
# its reboot entry gets it back.
phase_swap() {
  if swap_active; then
    printf '  swap already present\n'
    PHASE_STATUS="satisfied"
    if swapfile_active && ! swap_fstab_entry_present; then
      ensure_swap_fstab_entry
      PHASE_STATUS="changed"
    fi
    return 0
  fi

  local size
  size="$(swap_size_mb)"
  if [ "$size" -lt "$SWAP_MIN_MB" ]; then
    printf '  swap skipped: not enough free disk (a %s MB swapfile is under the %s MB minimum)\n' "$size" "$SWAP_MIN_MB"
    PHASE_STATUS="satisfied"
    return 0
  fi

  as_root fallocate -l "${size}M" "$SWAPFILE"
  as_root chmod 0600 "$SWAPFILE"
  as_root mkswap "$SWAPFILE"
  ensure_swap_fstab_entry
  if ! as_root swapon "$SWAPFILE"; then
    as_root rm -f "$SWAPFILE"
    remove_swap_fstab_entry
    printf '  swap skipped: not supported on this box (enabling swap was refused)\n'
    PHASE_STATUS="satisfied"
    return 0
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
  apt_update

  local base_pkgs=("${BASE_PACKAGES[@]}")
  if [ "$ACCESS" = "tailscale" ]; then
    base_pkgs+=(curl ca-certificates)
  fi

  local base_result ts_result="satisfied"
  apt_install "${base_pkgs[@]}"
  base_result="$APT_RESULT"

  if [ "$ACCESS" = "tailscale" ]; then
    ensure_tailscale_repo
    apt_update
    apt_install tailscale
    ts_result="$APT_RESULT"
  fi

  if [ "$base_result" = "satisfied" ] && [ "$ts_result" = "satisfied" ]; then
    PHASE_STATUS="satisfied"
  else
    PHASE_STATUS="changed"
  fi
}

# phase_tmux_oom_policy keeps a tmux pane's scope running when the kernel
# OOM-kills one of its processes, so the kill takes out only that process and
# not the pane's shell and with it the whole tmux session. It writes
# OOMPolicy=continue into a drop-in for every tmux pane scope on the box. It is
# check-before-change: a drop-in that already matches, with no reload left
# pending, is left untouched and the phase reports already-satisfied. A reload
# that failed is retried on the next run. It adapts to the box rather than
# failing setup: a systemd that cannot honour OOMPolicy= on a scope skips it with
# a note.
phase_tmux_oom_policy() {
  local want='[Scope]
OOMPolicy=continue'

  local version
  version="$(systemd_version)"
  if ! [[ "$version" =~ ^[0-9]+$ ]] || [ "$version" -lt "$SCOPE_OOM_POLICY_MIN_SYSTEMD" ]; then
    printf '  tmux-oom-policy skipped: systemd %s does not support OOMPolicy= on scopes (it needs %s or later)\n' "$version" "$SCOPE_OOM_POLICY_MIN_SYSTEMD"
    PHASE_STATUS="satisfied"
    return 0
  fi

  if [ "$(as_root cat "$SMITH_TMUX_SCOPE_DROPIN" 2>/dev/null || true)" = "$want" ] \
    && ! as_root test -e "$TMUX_OOM_RELOAD_PENDING"; then
    PHASE_STATUS="satisfied"
    return 0
  fi

  as_root touch "$TMUX_OOM_RELOAD_PENDING"
  local tmp
  tmp="$(mktemp)"
  printf '%s\n' "$want" >"$tmp"
  as_root mkdir -p "$(dirname "$SMITH_TMUX_SCOPE_DROPIN")"
  as_root install -m 0644 "$tmp" "$SMITH_TMUX_SCOPE_DROPIN"
  rm -f "$tmp"
  reload_user_managers
  as_root rm -f "$TMUX_OOM_RELOAD_PENDING"
}

# systemd_version prints the box's systemd version number, such as 255, or
# nothing when systemctl does not report one.
systemd_version() {
  systemctl --version 2>/dev/null | awk 'NR == 1 && $1 == "systemd" { print $2 }' || true
}

# reload_user_managers has every running user manager reload its configuration,
# so tmux scopes started after setup pick up a new drop-in without a reboot or a
# fresh login. A manager reloads on SIGHUP, which reaches it without its user's
# D-Bus session. It fails when the running managers cannot be listed, since then
# none of them is known to have reloaded.
reload_user_managers() {
  local units unit
  if ! units="$(as_root systemctl list-units --type=service --state=running --plain --no-legend 'user@*.service')"; then
    echo "tmux-oom-policy: could not list the running user managers to reload them" >&2
    return 1
  fi
  for unit in $(awk '{ print $1 }' <<<"$units"); do
    reload_user_manager "$unit"
  done
}

# reload_user_manager signals the user manager $1 to reload. A manager that has
# exited since it was listed needs no reload and is skipped; one still running
# that cannot be signalled fails it.
reload_user_manager() {
  local unit="$1"
  if as_root systemctl kill --kill-whom=main --signal=SIGHUP "$unit"; then
    return 0
  fi
  if as_root systemctl is-active --quiet "$unit"; then
    echo "tmux-oom-policy: could not signal the running user manager ${unit} to reload" >&2
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
