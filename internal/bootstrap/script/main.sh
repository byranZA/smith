# main: the subcommand dispatch, and the call that runs it.

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
    tailscale-status)
      tailscale_status
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
