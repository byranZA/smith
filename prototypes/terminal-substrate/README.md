# PROTOTYPE — terminal substrate spike (smith issue #53) — THROWAWAY

Proves the default terminal substrate for smith sessions: **one `tmux` session,
exposed at two access levels over `ttyd`** — read-only *observe* and writable
*interact* — matching `CONTEXT.md`: *"Observation and takeover are the same tmux
session at two access levels."*

Not production code. Deleted from `main`; lives only on its capture branch.

## Run

```sh
brew install tmux ttyd        # one-time
./spike.sh up                 # agent + always-on OBSERVE (read-only)
./spike.sh jumpin             # spawn the transient writable INTERACT
./spike.sh detach             # drop the writable surface, back to observe-only
./spike.sh status
./spike.sh down               # tears everything down
```

- **OBSERVE**  `http://127.0.0.1:7681` — read-only, **always on**
- **INTERACT** `http://127.0.0.1:7682` — writable, **spawned on jump-in**, exits on detach

The pane runs `agent-standin.py`: a fake agent that heartbeats forever and
echoes any line handed to it (`⟵ RECEIVED: …`).

## Drive it (the point of the spike)

1. `./spike.sh up`, open **OBSERVE**. Watch the heartbeats. Click in, type, hit
   Enter — **nothing lands**. You're watching, not touching.
2. `./spike.sh jumpin`, open **INTERACT**. Type a line + Enter — the agent prints
   `⟵ RECEIVED …`. You've jumped in.
3. Flip back to **OBSERVE** — the line you sent is *already there*. Same session.
4. Close the INTERACT tab (or `./spike.sh detach`) — that's "detach back to
   observe". The writable surface is gone; OBSERVE keeps running.

## Substrate design (locked in #53)

Default is **observe-only**: one always-on read-only `ttyd`. A **writable `ttyd`
is spawned on jump-in and torn down on detach** — not a second always-on
instance. `ttyd --once` makes this automatic: it accepts one client and exits the
moment that client disconnects, so closing the jump-in tab self-cleans the
writable surface.

| Level    | lifetime            | ttyd flags        | tmux attach | result                          |
|----------|---------------------|-------------------|-------------|---------------------------------|
| OBSERVE  | always on           | *(no `-W`)*       | `attach -r` | input never forwarded, two ways |
| INTERACT | spawned on jump-in  | `-W --once`       | `attach`    | keystrokes reach the pty; exits on detach |

Both bind **loopback only** (`-i 127.0.0.1`) — per research #55, reached from
elsewhere via `ssh -L` or `tailscale serve`, never a ufw port.

`set -g window-size latest` on the session so the most-recently-active client
dictates size — avoids tmux's "smallest client wins" cramping when an observe
and an interact client differ in size.

## Findings (headless verification)

- **Read-only genuinely blocks input.** 26 chars typed into OBSERVE in a real
  browser → **0 bytes** reached the pty. ttyd logs `will start in readonly mode`;
  the tmux client also reports read-only (`-r`).
- **Writable delivers.** Chars typed into INTERACT reach the writable pty; a line
  submitted with Enter surfaces as `⟵ RECEIVED …`, visible live in OBSERVE too.
- **Same session, survives clients.** Observe and interact attach the *same*
  `smith-spike` session; it keeps running as clients come and go — "detach" is
  just closing the writable connection.
- **Automation caveat (not a substrate limit):** a synthetic browser `Enter`
  didn't flush the line to the pty; a real human Enter (and `tmux send-keys …
  Enter`) both do. The buffered chars proved the bytes arrived — only the
  synthetic newline was dropped. This is why the human-in-the-loop drive above
  is the real confirmation.

## Design notes for productionization (react to these)

- **Two ttyd instances vs one:** the spike runs two (simplest). On a box, smith
  could keep the read-only observe ttyd always-on and spawn a writable attach
  on demand for jump-in, or run a single writable ttyd gated by network path.
- **Where `-r` lives:** the substrate's primary lock is ttyd's `-W` (absent =
  read-only); tmux `-r` is a redundant second lock. Keep both.
