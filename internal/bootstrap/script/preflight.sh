# preflight: the read-only preflight gate and the capability guard setup
# runs before it mutates anything.

# REQUIRED_CAPABILITIES lists the capabilities smith depends on that the OS floor
# gate does not prove, paired with the command that proves each is present:
#   apt      — drives the packages phase (apt-get)
#   systemd  — drives the service phases and reloads (systemctl)
# The setup guard checks these up front and fails naming the missing capability,
# rather than letting a phase abort deep inside with a raw "command not found".
# Each entry is a "capability:command" pair.
REQUIRED_CAPABILITIES=("apt:apt-get" "systemd:systemctl")

# CAPABILITY_MISSING_PREFIX is the sentinel the guard prints on stderr for a
# missing capability. The Go side (bootstrap package's missingCapabilityPrefix)
# parses it to lift the capability's name into an interpreted failure headline;
# keep the two in sync.
CAPABILITY_MISSING_PREFIX="required capability missing: "

# preflight emits the facts the Go side needs to gate the box and to give the
# memory advisory. It is a read-only probe: it changes nothing.
preflight() {
  echo "smith-preflight version=${SMITH_BOOTSTRAP_VERSION}"
  echo "privilege=$(detect_privilege)"
  echo "mem-total-kb=$(meminfo_kb MemTotal)"
  echo "os-release-begin"
  cat /etc/os-release
  echo "os-release-end"
}

# require_capabilities checks that every capability smith depends on is present
# before setup mutates anything, so a box that clears the OS floor but lacks a
# needed capability fails early with the capability named — not a raw
# "command not found" surfaced from deep inside a later phase. It is read-only: it
# probes command presence and changes nothing, and returns non-zero (aborting
# setup under set -e) after naming the first missing capability on stderr.
require_capabilities() {
  local entry cap cmd
  for entry in "${REQUIRED_CAPABILITIES[@]}"; do
    cap="${entry%%:*}"
    cmd="${entry#*:}"
    if ! command -v "$cmd" >/dev/null 2>&1; then
      printf '%s%s (the %s command was not found on the box)\n' \
        "$CAPABILITY_MISSING_PREFIX" "$cap" "$cmd" >&2
      return 1
    fi
  done
}
