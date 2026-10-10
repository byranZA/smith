# setup: the setup subcommand: its arguments, its exit cleanup, the phase run,
# and restoring the run state from the marker.

# remove_shipped_dir removes the directory named by --remove-dir, and nothing
# when none was named. It is setup's EXIT trap, so it keeps the exit status it
# was called with: the removal is best effort, and a cleanup that fails never
# fails the command.
remove_shipped_dir() {
  local rc=$?
  if [ -n "$REMOVE_DIR" ]; then
    rm -rf -- "$REMOVE_DIR" || true
  fi
  return "$rc"
}

# parse_setup_args reads the setup subcommand's flags: the access mode, the
# smith version, the box name and blueprint pointer to stamp into the marker,
# and the shipped directory to remove on exit.
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
      --blueprint)
        BLUEPRINT="${2:-}"
        shift 2
        ;;
      --name)
        BOX_NAME="${2:-}"
        shift 2
        ;;
      --public-ssh)
        PUBLIC_SSH="${2:-}"
        shift 2
        ;;
      --remove-dir)
        REMOVE_DIR="${2:-}"
        shift 2
        ;;
      *)
        echo "smith bootstrap setup: unknown argument: $1" >&2
        exit 64
        ;;
    esac
  done
}

# setup guards the required capabilities, writes the marker early, then runs each
# ordered phase. The capability guard runs before any mutation, so a box past the
# OS floor that lacks a needed capability fails named and untouched. In tailscale mode
# the terminal access phase is not a box-side streamed phase: enrolling the node
# and closing public SSH are gated on a live tailnet probe run from the admin
# side, so smith drives them separately (enroll, then close-public-ssh once the
# probe proves reach) and records the access phase then.
setup() {
  # Remove the shipped directory however the run ends, while this login still
  # reaches the box. The trap reads REMOVE_DIR at exit, so it is set before the
  # flags are parsed and still covers a run that rejects an argument.
  trap remove_shipped_dir EXIT
  parse_setup_args "$@"
  # Capability guard before any mutation: a box past the OS floor that still lacks
  # a needed capability fails here, named, rather than deep inside a later phase.
  require_capabilities
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

# load_marker_state restores the run parameters (access mode, smith version, box
# name, blueprint pointer) and completed phases from the existing marker, so a
# follow-up subcommand such as close-public-ssh rewrites the marker without
# clobbering what setup recorded.
load_marker_state() {
  [ -f "$MARKER" ] || return 0
  local am sv bp bn
  am="$(grep -o '"access_mode":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  sv="$(grep -o '"smith_version":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  bp="$(grep -o '"blueprint":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  bn="$(grep -o '"name":[[:space:]]*"[^"]*"' "$MARKER" | sed 's/.*"\([^"]*\)"$/\1/' || true)"
  [ -n "$am" ] && ACCESS="$am"
  [ -n "$sv" ] && SMITH_VERSION="$sv"
  [ -n "$bp" ] && BLUEPRINT="$bp"
  [ -n "$bn" ] && BOX_NAME="$bn"

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
