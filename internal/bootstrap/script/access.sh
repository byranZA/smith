# access: the access layer: the public-mode access phase and the tailscale-mode
# enroll, close-public-ssh and tailscale-status subcommands.

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

# tailscale_backend_state echoes Tailscale's BackendState (e.g. Running), or
# nothing when the daemon is unreachable or the field is absent. Read-only.
tailscale_backend_state() {
  tailscale status --json 2>/dev/null \
    | grep -o '"BackendState":[[:space:]]*"[^"]*"' \
    | sed 's/.*"\([^"]*\)"$/\1/' | head -n1 || true
}

# tailscale_status prints the box's tailnet IP as tailscale-ip=<ip> only when the
# node is already enrolled and Running, and nothing otherwise. It lets smith's
# access layer skip re-enrollment on a re-run of an already-reachable box instead
# of burning a fresh single-use auth key. It is query-only: it mutates nothing.
tailscale_status() {
  if [ "$(tailscale_backend_state)" != "Running" ]; then
    return 0
  fi
  local ip
  ip="$(tailscale ip -4 2>/dev/null | head -n1 || true)"
  if [ -n "$ip" ]; then
    printf 'tailscale-ip=%s\n' "$ip"
  fi
}
