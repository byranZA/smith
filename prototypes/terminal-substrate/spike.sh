#!/usr/bin/env bash
# PROTOTYPE — terminal-substrate spike for smith (issue #53). THROWAWAY.
#
# Substrate model (locked in #53): ONE tmux session (a stand-in "agent"), with a
# permanent read-only OBSERVE link, and a writable INTERACT link spawned ONLY on
# jump-in and torn down on detach — not a second always-on instance.
#
#   up       start the agent's tmux session + the always-on OBSERVE ttyd (read-only)
#   jumpin   spawn the transient writable INTERACT ttyd (ttyd --once: self-exits on detach)
#   detach   tear the writable ttyd down explicitly (equivalent to closing the tab)
#   down     stop everything
#
#   OBSERVE  (read-only): ttyd NO -W + tmux `attach -r`  → input blocked, two ways
#   INTERACT (writable) : ttyd -W --once + tmux `attach`  → jump in; exits on detach
#
# Both bind loopback only (127.0.0.1) — per research #55, reached from elsewhere
# via `ssh -L` or `tailscale serve`, never a ufw port.
set -euo pipefail

SESSION=smith-spike
OBSERVE_PORT=7681
INTERACT_PORT=7682
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT="$HERE/agent-standin.py"
RUN="$HERE/.run"          # pidfiles (gitignored throwaway)
mkdir -p "$RUN"

_need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1" >&2; exit 1; }; }
_alive() { [ -f "$RUN/$1.pid" ] && kill -0 "$(cat "$RUN/$1.pid")" 2>/dev/null; }

up() {
  _need tmux; _need ttyd; _need python3

  # 1. long-running "agent" inside a tmux session
  if tmux has-session -t "$SESSION" 2>/dev/null; then
    echo "session '$SESSION' already up"
  else
    tmux new-session -d -s "$SESSION" "python3 -u '$AGENT'"
    # most-recently-active client dictates size — avoids tmux's "smallest client
    # wins" cramping when the observe and jump-in clients differ in size.
    tmux set -t "$SESSION" -g window-size latest 2>/dev/null || true
    echo "started tmux session '$SESSION' running the stand-in agent"
  fi

  # 2. OBSERVE — permanent, read-only. No -W on ttyd (input never forwarded) AND
  #    tmux `attach -r` (read-only client). Belt and suspenders.
  if _alive observe; then
    echo "observe ttyd already up"
  else
    ttyd -i 127.0.0.1 -p "$OBSERVE_PORT" -t disableLeaveAlert=true \
      tmux attach -t "$SESSION" -r >"$RUN/observe.log" 2>&1 &
    echo $! >"$RUN/observe.pid"
  fi
  echo "OBSERVE  (read-only): http://127.0.0.1:$OBSERVE_PORT   [always on]"
  echo "jump in with: $0 jumpin"
}

jumpin() {
  _need ttyd
  tmux has-session -t "$SESSION" 2>/dev/null || { echo "run '$0 up' first" >&2; exit 1; }
  if _alive interact; then
    echo "already jumped in: http://127.0.0.1:$INTERACT_PORT"
    return
  fi
  # INTERACT — transient, writable. -W + writable tmux attach. --once: ttyd exits
  # the moment this single client disconnects, so "detach" self-cleans the writable
  # surface and the box falls back to observe-only.
  ttyd -i 127.0.0.1 -p "$INTERACT_PORT" -W --once -t disableLeaveAlert=true \
    tmux attach -t "$SESSION" >"$RUN/interact.log" 2>&1 &
  echo $! >"$RUN/interact.pid"
  echo "INTERACT (writable) : http://127.0.0.1:$INTERACT_PORT   [spawned; exits on detach]"
  echo "type a line + Enter; close the tab (or '$0 detach') to drop back to observe."
}

detach() {
  if _alive interact; then
    kill "$(cat "$RUN/interact.pid")" 2>/dev/null || true
  fi
  rm -f "$RUN/interact.pid"
  echo "detached — writable ttyd gone; OBSERVE keeps running"
}

down() {
  detach >/dev/null 2>&1 || true
  if _alive observe; then kill "$(cat "$RUN/observe.pid")" 2>/dev/null || true; fi
  rm -f "$RUN/observe.pid"
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  echo "torn down (ttyd stopped, tmux session killed)"
}

status() {
  tmux has-session -t "$SESSION" 2>/dev/null && echo "tmux session '$SESSION': UP" || echo "tmux session '$SESSION': down"
  _alive observe  && echo "OBSERVE  ttyd: UP (pid $(cat "$RUN/observe.pid"))"  || echo "OBSERVE  ttyd: down"
  _alive interact && echo "INTERACT ttyd: UP (pid $(cat "$RUN/interact.pid")) — jumped in" || echo "INTERACT ttyd: down — observe-only"
}

case "${1:-}" in
  up) up ;;
  jumpin) jumpin ;;
  detach) detach ;;
  down) down ;;
  status) status ;;
  *) echo "usage: $0 {up|jumpin|detach|down|status}" >&2; exit 2 ;;
esac
