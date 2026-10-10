# probe: the read-only probe subcommand that reports the box's live facts.

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
  if grep -qi 'Status: active' <<<"$status"; then
    echo "ufw-active=active"
  else
    echo "ufw-active=inactive"
  fi
  if grep -qi 'deny (incoming)' <<<"$status"; then
    echo "ufw-default-deny=deny"
  else
    echo "ufw-default-deny=allow"
  fi
  if grep -qE '22/tcp[[:space:]]+ALLOW' <<<"$status"; then
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
  if grep -q 'APT::Periodic::Unattended-Upgrade "1"' <<<"$conf"; then
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
  state="$(tailscale_backend_state)"
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
# marker (in a delimited block), the total memory and swap the status report
# notes without reconciling, whether the smith user exists and still holds
# passwordless sudo, and — only when sudo is available — the firewall, sshd,
# fail2ban, and auto-updates facts. When passwordless sudo is lost, only
# passwordless-sudo=no is emitted; the reconciler's lost-sudo branch owns the
# unverifiable verdict for the root-only facts, which are simply absent.
# It is query-only: it mutates nothing.
probe() {
  echo "marker-begin"
  cat "$MARKER" 2>/dev/null || true
  echo "marker-end"

  echo "mem-total-kb=$(meminfo_kb MemTotal)"
  echo "swap-total-kb=$(meminfo_kb SwapTotal)"

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
  fi

  probe_access
}
