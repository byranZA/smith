# Terminal substrate landscape: should smith change its default?

Research ticket resolution. Surveys the terminal-substrate landscape against smith's
locked default (**tmux + ttyd**) and its actual needs: an observe -> jump-in -> detach
loop with a read-only observe level and a writable interact level over the *same*
persistent session, exposed **loopback-only** and reached purely over `ssh -L` or
`tailscale serve` — never an open inbound port, never a hosted/third-party relay.

## Verdicts

**Default: keep tmux + ttyd.** Nothing in the landscape wins on *both* smith's needs
*and* the loopback-only / no-relay exposure constraint. The one genuine contender —
Zellij 0.43+ with its built-in web client — collapses the two daemons into a single
binary and natively models read-only vs. writable access via tokens over a loopback
bind. That is a real architectural advantage and the strongest reason to offer an
opt-in. But it does not *decisively* beat the incumbent: it is young (first shipped
Aug 2025), its "same session at two access levels" story is token-based rather than
smith's two-independent-locks model, and tmux+ttyd is boring, proven, and already
wired. The collaborative all-in-ones (sshx, tmate, upterm) are disqualified as defaults
because they are relay-oriented by design; only tmate/upterm can be self-hosted at
meaningful operational cost, and sshx cannot be self-hosted at all.

**Opt-in seam: defer a general plug system to post-v1; ship "tmux+ttyd only, seam
noted."** The `terminal` field in Blueprint should exist and be documented, but v1
should recognize exactly one value (`tmux+ttyd`) and reject others. A pluggable
substrate contract is cheap to *describe* (below) but expensive to *honor* — each
alternative has a materially different observe/interact and exposure model, so a real
plug system means N tested integrations, not one interface. Zellij is the obvious
*second* implementation when demand appears; the contract below is written so adding it
later is not a rewrite.

## Per-tool comparison

Criteria: **RO/RW same session** = can expose a read-only observe level and a writable
interact level over one underlying session. **Persist** = session survives client
disconnect. **Loopback + no relay** = can bind loopback-only and be reached via `ssh -L`
/ `tailscale serve` with no inbound port and no third-party relay. **Op cost** = extra
daemons / install / config surface.

### Layer 1 — persistence / multiplexer

| Tool | RO/RW same session | Persist | Loopback + no relay | Op cost | Notes |
|---|---|---|---|---|---|
| **tmux** | Yes — `attach -r` (read-only) vs `attach` (writable) on the same session | Yes | N/A (not a network tool; a bridge exposes it) | Low; ubiquitous | The incumbent's persistence layer. RO/RW is a per-attach property, exactly smith's "two access levels, one session" model. [1] |
| **Zellij** | Partial at the mux layer, **Yes via its own web client** (see fused tools) | Yes (incl. session resurrection) | Yes (web client binds `127.0.0.1` by default) | Low-medium; single binary but newer | The only mux with a first-party web bridge; evaluated as a fused tool below. [7][8] |
| **GNU screen** | Yes, but clunky — multiuser ACLs (`multiuser on`, `aclchg user -w`) grant read-only to *named OS users* attaching via `screen -x` | Yes | N/A (needs a bridge) | Low install; high config friction (setuid, ACLs) | RO is an OS-user ACL, not a clean per-attach flag; awkward for a single-operator jump-in model. [9] |
| **dtach / abduco** | No usable read-only viewer level | Yes (minimal detach/reattach) | N/A (needs a bridge) | Very low | Tiny detach helpers; abduco has no view-only sharing (open question upstream), dtach tracks no screen state. Too thin for the observe level. [10] |

### Layer 2 — web-exposure bridge

| Tool | RO/RW | Loopback + no relay | Op cost | Notes |
|---|---|---|---|---|
| **ttyd** | Read-only by **default**; `-W`/`--writable` opts into write. `--once` exits on the single client's disconnect | Yes — no relay; loopback bind via `-i lo` / `-i 127.0.0.1` (passed to libwebsockets) | Low; single static binary, apt/brew | The incumbent's bridge. Smith's two-ttyd design (always-on `-W`-less observe + on-demand `-W --once` interact) maps directly onto these flags. See caveat on `-i` below. [2][3] |
| **gotty** | Read-only by default; `-w`/`--permit-write` opts in; `--address` sets bind IP (default `0.0.0.0`) | Yes — own WebSocket server, no relay | Low; single Go binary | Functionally a ttyd predecessor (ttyd is "inspired by gotty"); effectively unmaintained. No advantage over ttyd. [4] |
| **wetty** | No native read-only mode; runs a login/ssh shell in-browser | Yes (standalone) but heavier | Medium; Node.js >=20 runtime | Node dependency and no RO level make it strictly worse than ttyd for smith. [5] |
| **xterm.js DIY** | Whatever you build | Yes | High (you own the server) | xterm.js is the client lib the others embed; rolling your own is all cost, no benefit here. |

### Layer 3 — all-in-one / collaborative session-sharing daemons

| Tool | RO/RW | Loopback + no relay | Op cost | Notes |
|---|---|---|---|---|
| **tmate** | Yes — emits `ssh session-ro` and `web session-ro` alongside RW strings | **No by default** — connects out to public relays (`*.tmate.io`); self-hostable via `tmate-ssh-server` + `set tmate-server-host` | Medium-high if self-hosted (run + maintain an SSH relay) | Fork of tmux; the session data path *is* the relay. Even self-hosted it is a relay, not a loopback bind — architecturally opposite to smith's model. [11][12][13] |
| **sshx** | Not documented as read-only | **No** — "Self-hosted deployments are not supported"; requires the sshx.io global mesh | N/A | E2E-encrypted through a relay, but hard relay dependency and no self-host = disqualified as default *and* weak as opt-in. [6] |
| **upterm** | `--read-only` is SFTP-scoped, not a viewer level | **No by default** — routes through `uptermd.upterm.dev`; self-hostable (k8s/fly/systemd/docker) | High if self-hosted | Reverse SSH tunnel to an uptermd server; same relay-shaped objection as tmate. [14] |
| **herdr (+ herdr-web)** | **No** — herdr-web has no read-only vs. writable level; origin-based CORS, all allowed clients get full control | Bridge binds `127.0.0.1:8787` by default (loopback OK), no herdr.dev dependency | Medium; two moving parts, both young | herdr is a tmux-like *agent* multiplexer; herdr-web is an explicitly unofficial, early-stage bridge ("not associated with... the official Herdr project", "runtime/API shape is expected to change"). Lacks the RO/RW split that is the core of smith's model. [15][16] |

## The pluggability contract

The `terminal` field in Blueprint marks the seam. A substrate is pluggable into smith
iff it can satisfy **all four** clauses; each maps to a concrete capability smith drives:

1. **Persistent session.** A named session that outlives every client; disconnect/detach
   never ends it. Only agent-directed `exit` / `Ctrl-D` / `Ctrl-C` inside the session
   ends it. (smith must be able to create/name/attach without owning the process
   lifetime.)
2. **Read-only observe level.** An always-on attach that *cannot* inject input, safe to
   leave running for any number of simultaneous watchers.
3. **Writable interact level.** An on-demand attach over the *same* session that accepts
   input, and that self-cleans when the single interactive client leaves (so a dropped
   jump-in never leaves a lingering writable door open).
4. **Loopback exposure, no relay.** Binds `127.0.0.1` only; reachable via `ssh -L` and
   `tailscale serve`; requires no inbound firewall port and no hosted/third-party relay.

Contract satisfaction by candidate:

| Candidate | (1) Persist | (2) RO observe | (3) RW interact, self-clean | (4) Loopback, no relay | Pluggable? |
|---|---|---|---|---|---|
| **tmux + ttyd** (incumbent) | Yes | Yes (`attach -r` + `-W`-less ttyd) | Yes (`attach` + `ttyd -W --once`) | Yes | **Yes — reference impl** |
| **Zellij** (built-in web client) | Yes | Yes (read-only token) | Yes (login token; per-client cursor) | Yes (`127.0.0.1:8082`) | **Yes — best second impl** |
| GNU screen + ttyd | Yes | Via multiuser ACL (clunky) | Partial | Yes (ttyd) | Marginal — RO model is OS-user ACLs |
| dtach/abduco + ttyd | Yes | No clean RO viewer | Partial | Yes (ttyd) | No — fails (2) |
| gotty (as bridge over tmux) | via tmux | Yes (default) | Yes (`-w`) but no `--once`-style self-clean | Yes | Mostly — strictly dominated by ttyd |
| wetty | No | No | n/a | Yes | No — fails (1),(2) |
| tmate | Yes | Yes (`-ro`) | Yes | **No** (relay by design) | No — fails (4) |
| sshx | Partial | Not documented | Yes | **No** (no self-host) | No — fails (4) |
| upterm | Unclear | No (viewer) | Yes | **No** default; self-host heavy | No — fails (4) |
| herdr + herdr-web | Yes | **No** | No RO/RW split | Yes (loopback) | No — fails (2),(3) |

**Recommendation:** ship v1 as `terminal: tmux+ttyd` recognized, all other values
rejected with a clear error. Keep the field and this contract in the docs so the seam is
real and Zellij can land later as the second implementation without a redesign. A general
N-substrate plug system is not worth v1 because "pluggable" here means N *tested*
integrations with materially different exposure models, not one shared interface — the
cost is integration and testing, which the contract text does not remove.

## Caveats / surprises worth a second look

- **ttyd `-i` phrasing.** The man page describes `-i`/`--interface` as an *interface
  name* (`eth0`) or *UNIX socket path*, and does **not** advertise IP addresses. In
  practice ttyd passes this straight to libwebsockets, which binds an IP if given one, so
  `-i 127.0.0.1` (or `-i lo`) does achieve loopback-only. smith's config already relies
  on `-i 127.0.0.1`; worth a one-line confirmation against the installed ttyd/lws
  version rather than trusting the man page wording. [3]
- **Ecosystem noise around "herdr."** Multiple distinct repos carry the name (an
  official-looking `SuperCodeAgents/herdr-terminal`, `herdrdev/herdr`, plus the
  *unofficial* `kcosr/herdr-web`) with inconsistent star counts across write-ups. Treat
  herdr as young and unsettled; its web bridge is explicitly experimental and lacks the
  RO/RW split. [15][16]
- **Zellij is the only real "collapse two daemons into one" story** that still honors the
  exposure constraint — mux + web bridge in a single binary, loopback by default, native
  read-only vs. login tokens, session resurrection. It is the clear candidate if smith
  ever wants to drop the separate ttyd process. Its youth is the only thing keeping it out
  of the default slot. [7][8]

## References

1. tmux manual — `attach-session -r` (read-only) / writable attach: https://man.openbsd.org/tmux.1
2. ttyd README — options incl. `-W/--writable` (readonly default), `-o/--once`, no relay: https://github.com/tsl0922/ttyd
3. ttyd(1) man page — `-i/--interface` and `-W` wording: https://manpages.ubuntu.com/manpages/noble/man1/ttyd.1.html
4. gotty README — `-w/--permit-write` (read-only default), `--address`, own WebSocket server: https://github.com/yudai/gotty
5. wetty README — xterm.js over http/https, Node.js >=20, login/ssh shell: https://github.com/butlerx/wetty
6. sshx README — global mesh relay; "Self-hosted deployments are not supported": https://github.com/ekzhang/sshx
7. Zellij 0.43.0 release notes — web client, per-session URL, read-only tokens, session resurrection: https://zellij.dev/news/web-client-multiple-pane-actions/
8. Zellij web client docs — binds `127.0.0.1:8082` default, `--create-read-only-token`, HTTPS for non-loopback, single binary: https://zellij.dev/documentation/web-client
9. GNU screen multiuser mode — ACL-based read-only via `multiuser on` / `aclchg`: https://aperiodic.net/screen/multiuser
10. abduco — no view-only session sharing (upstream question), dtach tracks no screen state: https://dev.suckless.narkive.com/5ZmfppiV/abduco-read-only-connect-to-session-from-other-user
11. tmate README — fork of tmux, instant pairing: https://github.com/tmate-io/tmate
12. tmate-ssh-server — self-hostable relay; server-side of tmate.io: https://github.com/tmate-io/tmate-ssh-server
13. tmate — `show-messages` yields ssh/web read-only strings; `tmate-server-host` self-host config: https://tmate.io/
14. upterm README — default relay `uptermd.upterm.dev`, `--server` / self-host options, reverse SSH tunnel: https://github.com/owenthereal/upterm
15. herdr — terminal agent multiplexer, persistent background session: https://github.com/SuperCodeAgents/herdr-terminal
16. herdr-web — unofficial, experimental browser bridge; loopback `127.0.0.1:8787`, origin-based access (no RO/RW split): https://github.com/kcosr/herdr-web
