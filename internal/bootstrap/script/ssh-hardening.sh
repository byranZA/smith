# ssh-hardening: the hardening sshd drop-in, its self-reverting self-test and
# the phase that applies them.

# The sshd drop-in that hardens SSH (PermitRootLogin no, PasswordAuthentication
# no). SMITH_SSHD_DROPIN overrides it for tests; production always uses the
# sshd_config.d default. The 01- prefix sorts it ahead of distro drop-ins (e.g.
# cloud-init's 50-cloud-init.conf) so sshd's first-match-wins honors the hardened
# values.
SMITH_SSHD_DROPIN="${SMITH_SSHD_DROPIN:-/etc/ssh/sshd_config.d/01-smith-hardening.conf}"

# PROBE_MARKER tags the throwaway "probe key" the ssh-hardening self-test appends to
# smith's authorized_keys, so cleanup can strip it back out by an exact token even
# after a crash. It rides in the key's comment field, so the whole appended line
# carries it — keep it a single whitespace-free token (it is an SSH key comment).
PROBE_MARKER="smith-ssh-hardening-selftest-probe"

# reload_sshd asks systemd to reload the ssh service so a changed sshd config
# takes effect without dropping the connections smith is reaching the box over.
reload_sshd() {
  as_root systemctl reload ssh
}

# selftest_strip_probe_lines removes any probe-key lines (tagged with PROBE_MARKER)
# from smith's authorized_keys. It runs before the probe as a defensive strip — so a
# probe line orphaned by an earlier crashed run can never stay authorized across a
# re-run — and again during cleanup. A no-op when the file is absent or carries no
# marker, so it is safe to call repeatedly.
selftest_strip_probe_lines() {
  local authkeys="$1"
  as_root test -f "$authkeys" 2>/dev/null || return 0
  as_root grep -q -- "$PROBE_MARKER" "$authkeys" 2>/dev/null || return 0
  local tmp
  tmp="$(mktemp)"
  as_root grep -v -- "$PROBE_MARKER" "$authkeys" >"$tmp" 2>/dev/null || true
  as_root cp "$tmp" "$authkeys"
  as_root chmod 0600 "$authkeys"
  rm -f "$tmp"
}

# selftest_cleanup scrubs the probe artifacts: the throwaway keypair's tmp dir and
# the probe line appended to smith's authorized_keys. Idempotent, so it serves as
# both the inline cleanup and the trap handler.
selftest_cleanup() {
  local tmpdir="$1" authkeys="$2"
  [ -n "${tmpdir:-}" ] && rm -rf "$tmpdir"
  selftest_strip_probe_lines "$authkeys"
}

# selftest_probe mints the probe keypair, authorizes its public half for smith, and
# drives the loopback login. Its exit status is the login's: success means the
# hardened config still admits a pubkey login for smith.
selftest_probe() {
  local tmpdir="$1" authkeys="$2"
  ssh-keygen -t ed25519 -N '' -C "$PROBE_MARKER" -f "${tmpdir}/probe" >/dev/null 2>&1 || return 1
  as_root tee -a "$authkeys" <"${tmpdir}/probe.pub" >/dev/null || return 1
  ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
    -o IdentitiesOnly=yes -i "${tmpdir}/probe" \
    smith@localhost true
}

# ssh_hardening_selftest proves the hardened sshd config still admits a pubkey login
# for smith before the phase trusts it. It authenticates with a "probe key" — a
# throwaway keypair minted on the box for this test — so the gate depends on nothing
# of the operator's (key, agent, forwarding, passphrase, ssh_config):
#   1. validate the merged config (`sshd -t`) and reload sshd,
#   2. assert the effective config still enables pubkey auth for smith (`sshd -T`),
#   3. ssh-keygen a throwaway keypair, append its public half to smith's real
#      authorized_keys (a PROBE_MARKER-tagged line, so the live handshake exercises
#      that file's perms/ownership/StrictModes), then loopback `ssh smith@localhost`
#      with the private half (-i, IdentitiesOnly=yes).
# The appended line touches smith's real authorized_keys, so cleanup (scrub the
# keypair + strip the marker line) runs on every path — inline on return and via a
# trap on interrupt/abort — and a defensive strip up front clears any line orphaned
# by a crashed prior run. The trap chains setup's remove_shipped_dir, and setup's
# EXIT trap is restored afterwards, so the shipped directory is still removed. Any step failing returns non-zero so the caller reverts the
# drop-in. Smith's admin-side `ssh smith@host` reconnect stays a post-confirm, not
# this gate.
ssh_hardening_selftest() {
  local authkeys="${SMITH_HOME}/.ssh/authorized_keys"
  selftest_strip_probe_lines "$authkeys"

  as_root sshd -t || return 1
  reload_sshd || return 1
  local effective
  effective="$(as_root sshd -T -C "user=${SMITH_USER},host=localhost,addr=127.0.0.1" 2>/dev/null || true)"
  grep -qi '^pubkeyauthentication yes' <<<"$effective" || return 1

  local tmpdir
  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "selftest_cleanup '$tmpdir' '$authkeys'; remove_shipped_dir" EXIT INT TERM

  local rc=0
  selftest_probe "$tmpdir" "$authkeys" || rc=1

  selftest_cleanup "$tmpdir" "$authkeys"
  trap - INT TERM
  trap remove_shipped_dir EXIT
  return "$rc"
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
