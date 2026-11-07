# Published Docker ports default to loopback, and the step knows Docker by name

> Narrows the rule in [ADR-0012](./0012-the-repo-file.md) that smith knows no service by name, for one case: Docker, which defeats the firewall smith sets up.

## Context

A port published without a host address binds `0.0.0.0`, and Docker routes that IPv4 traffic around ufw. A box with default-deny ufw and `machine status` reporting a match was still open from outside ([#696](https://github.com/byranZA/smith/issues/696)). [#545](https://github.com/byranZA/smith/issues/545) chose the general `groups:` over any Docker-specific blueprint key, so the smith user could join `docker` without smith naming it.

## Decision

**The workspace stage has a docker step that sets `ip` and `default-network-opts.bridge` to loopback in `/etc/docker/daemon.json`, and `machine status` warns of exposed bindings.**

- **It is Docker-specific because the fault is.** `groups:` is a neutral membership list with nothing to say about the firewall. Docker rewrites iptables itself, so no firewall rule smith adds to ufw can close this: only Docker's own defaults can. The step is not a blueprint key. It runs on every converge, detects Docker on the box, and does nothing without it.
- **Services reach the operator through an SSH tunnel.** A service that listens on loopback is not reachable from the operator's machine directly, and that is intended: the SSH door is the only way in ([ADR-0005](./0005-terminal-rides-the-ssh-door.md)). An operator who wants a port public says so with an explicit address.
- **An explicit binding is not overridden.** The step changes defaults. The status warning names what stays exposed and does not affect the verdict, so a deliberately public port does not mark the box drifted forever.

## Considered options

- **Manage `DOCKER-USER` or another firewall chain.** Out of scope: it is Docker's chain to manage, and rules there depend on the Docker version and backend.
- **A blueprint key for Docker.** It puts the safe default behind an opt-in, and the box is exposed until the operator learns of it.
- **Reload instead of restart.** A reload does not apply these settings, as observed on a box.

## Consequences

- A converge that changes the config restarts the daemon, stopping containers without a restart policy. It does not detect a running loop or stack; revisit with the stack lease in [#697](https://github.com/byranZA/smith/issues/697).
- Networks that exist keep their old binding until recreated.
- Docker's nftables backend, Swarm and networks with IPv6 enabled are not handled.
