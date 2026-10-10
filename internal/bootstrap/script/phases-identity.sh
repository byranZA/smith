# phases-identity: the phases that decide who reaches the box: smith-user,
# smith-keys and firewall.

# The sudoers.d directory the smith-user phase writes smith's drop-in to.
# SMITH_SUDOERS_DIR overrides it for tests, matching SMITH_MARKER; production
# always uses the default.
SMITH_SUDOERS_DIR="${SMITH_SUDOERS_DIR:-/etc/sudoers.d}"

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
  grep -qE '22/tcp[[:space:]]+ALLOW' <<<"$status" || has_ssh_rule=0

  local ssh_ok=1
  if [ "$want_public_ssh" = "open" ] && [ "$has_ssh_rule" -eq 0 ]; then
    ssh_ok=0
  fi
  if [ "$want_public_ssh" = "closed" ] && [ "$has_ssh_rule" -eq 1 ]; then
    ssh_ok=0
  fi

  if grep -qi 'Status: active' <<<"$status" \
    && grep -qi 'deny (incoming)' <<<"$status" \
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
