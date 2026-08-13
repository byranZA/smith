# The session terminal rides the SSH door; nothing is exposed

## Context

Bootstrap leaves the box hardened: ufw default-deny inbound, `PermitRootLogin no`,
`PasswordAuthentication no`, and — in `--access=tailscale` — public SSH closed outright
([ADR-0001](./0001-tailscale-ssh-keyless-tagged-node.md),
[ADR-0004](./0004-lockout-gate-on-box-self-reverting-self-test.md)). Work then happens in
[sessions](../../CONTEXT.md): a `tmux` session per worktree, driven by a human or a coding agent,
which the operator watches read-only and jumps into when the agent needs a hand.

Surfacing that terminal is the classic moment a hardened box gets un-hardened. The web-terminal
route (`ttyd`) is the obvious one — it puts a terminal in a browser tab, which buys a clickable
link from a phone — and every variant of it needs an answer to "what listens, on which interface,
authenticated how, reachable from where". The reference architecture (open a port, front it with
nginx/Caddy, terminate TLS, add Basic auth) opens a hole in the default-deny firewall on day one.

Two questions had to be settled before any of that: whether terminal reach is a **swappable
layer** in the sense `access` is (`--access=public` vs `--access=tailscale`, the one pluggable
choice layered over the base), and whether a web terminal is needed at all when reaching the box
already requires SSH.

## Decision

**v1 exposes no terminal listener.** Attaching to a session is a native SSH command driving
`tmux` on the box:

```
observe    ssh <box> -t tmux attach -t smith/<name> -r
interact   ssh <box> -t tmux attach -t smith/<name>
```

Three consequences fall out of that one line:

**Terminal reach is not a layer.** `access` earns that status because it is an independent
operator choice whose alternatives need materially different machinery — tailscale mode must
close the very door public mode leaves open, and the firewall phase is access-aware to make it
safe. Terminal reach fails both tests: the path is *derived* from the access mode rather than
chosen, and every candidate path pointed at the same substrate. So there is **no
`--terminal-access` flag, no terminal phase in the bootstrap sequence, and no terminal-aware seam
in the base layer.** `access` remains the one pluggable choice.

**The command is byte-identical in both access modes.** The access layer already decided which
door SSH walks through — the public IP, or the tailnet — and the terminal simply walks through
whichever is open. Identity and authorization are the SSH door smith already built: the operator's
key in public mode, Tailscale SSH plus tailnet ACL in tailscale mode. Nothing is provisioned, and
there is no second credential to reference.

**`ttyd` does not ship in v1**, so the v1 substrate is `tmux` alone. Observe and interact are two
access levels on one `tmux` session (`attach -r` vs `attach`) rather than two web servers
differentiated by `ttyd -W`.

The hardening is therefore preserved **by construction rather than by mitigation**: no port, no
tunnel, no reverse proxy, no TLS to terminate, no listener bound anywhere. There is nothing added
for a future audit to re-check.

## Considered options

- **Open a port + reverse proxy + TLS + Basic auth** — rejected outright, not deferred. It is the
  only design that punches a hole in the default-deny firewall, and in tailscale mode it
  re-exposes publicly exactly what that mode exists to close. It also introduces a Basic-auth
  password as a second credential to provision, against
  [*reference, not value*](./0002-secrets-are-references-not-values.md).
- **Loopback `ttyd` reached over an `ssh -L` tunnel** — rejected as *dominated*. It requires an
  authenticated SSH client to build the tunnel, and anyone holding one can run `tmux attach`
  directly for a strictly better terminal (native fonts, key handling, scrollback, copy/paste)
  with none of the machinery. In `--access=public`, SSH is the only door to the box, so every user
  in that mode has an SSH client by definition and the browser path serves nobody.
- **Loopback `ttyd` fronted by `tailscale serve`** — **deferred post-v1, not rejected.** It is a
  genuine capability: a tailnet-only `*.ts.net` URL with auto-TLS, tappable from a device with no
  SSH client. But mature mobile SSH clients (Termius, Blink) already cover the phone case, and
  `tmux attach -r` works inside them, so mobile *observe* was already solved; `tmux` also serves
  several simultaneous clients on one session, so multi-viewer is not a reason either. What
  remains is the locked-down-machine case, which does not pay for a per-session listener, port
  allocation, and a third tailnet prereq (the HTTPS-for-tailnet admin toggle, alongside
  `tagOwners` and `ssh`).
- **Terminal exposure as a swappable layer alongside `access`** — rejected: see the two tests
  above. The genuine extensibility axis is *which substrate*, not *which pipe*, and that already
  lives in the Blueprint `terminal` field.
- **Enforced read-only** (a restricted shell, or a forced-command SSH key that can only run
  `tmux attach -r`) — rejected for v1: substantial machinery to defend against the operator
  mistyping into their own box. See the consequence below.

## Consequences

- **Read-only is advisory, not a permission boundary.** `-r` is a flag on the `tmux` client, so
  anyone who can SSH as `smith` can attach writable regardless of what smith's CLI passes. Under
  `ttyd` the lock was server-side — input was never forwarded — and that property does **not**
  carry over. This is deliberate: read-only guards against *accidentally typing into an agent's
  session*, and the only holder of that key is the operator, on their own box. Recorded explicitly
  so it is never mistaken for a permission model, and so a future multi-operator story knows it is
  building the boundary from scratch.
- **HITL is unaffected.** The session verbs split on intent, not on driver: `start` connects
  writable (you are here to work) and is how you resume your own session, while `attach` defaults
  to read-only (you are here to watch). The failure mode of an accidental read-only `attach` is
  that typing does nothing and you reconnect ([issue #59](https://github.com/byranZA/smith/issues/59)).
- **The substrate contract loses its exposure clause.** It is three clauses, not four — persistent
  session, read-only observe, read-write interact — since exposure is now the SSH door's job
  rather than the substrate's. The Blueprint `terminal` field survives unchanged as the swap
  point; v1 recognizes `tmux`, and Zellij remains the obvious second implementation, though part
  of its appeal was its web client and that advantage only pays off if the browser path returns
  ([issue #64](https://github.com/byranZA/smith/issues/64)).
- **Adding a listener is the way to break this.** Any future feature that binds a port on the box
  — a web terminal, a status dashboard, a metrics endpoint — re-opens the question this ADR
  closed and must be weighed against the firewall invariant, not slipped in beside it.
- **Re-entry point if the browser case gets real:** the deferred design is intact in
  [issue #55](https://github.com/byranZA/smith/issues/55) (loopback bind, `tailscale serve`, auth
  model) and the `prototype/terminal-substrate` spike remains a working `ttyd` reference. It
  returns as its own effort, not as a resumption of this one.

See [issue #61](https://github.com/byranZA/smith/issues/61).
