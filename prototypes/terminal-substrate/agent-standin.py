#!/usr/bin/env python3
# PROTOTYPE — stand-in for a coding agent (smith issue #53). THROWAWAY.
#
# Heartbeats forever so an observer sees liveness, and echoes any line it is
# handed so you can *see* input landing. In OBSERVE mode your keystrokes go
# nowhere; jump in (INTERACT) and the agent reports what it RECEIVED.
import sys, threading, time, itertools


def heartbeat():
    for n in itertools.count(1):
        print(f"[agent] working… tick {n}  {time.strftime('%H:%M:%S')}", flush=True)
        time.sleep(2)


threading.Thread(target=heartbeat, daemon=True).start()
print("[agent] READY — in OBSERVE you can't type; jump in (INTERACT) and hand me a line.", flush=True)
try:
    for line in sys.stdin:
        print(f"[agent] ⟵ RECEIVED: {line.rstrip()!r} — acting on it now", flush=True)
except KeyboardInterrupt:
    pass
print("[agent] stdin closed — exiting.", flush=True)
