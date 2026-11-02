# The config stage applies the staged tree on the box

> Amends [ADR-0008](./0008-smith-runs-on-the-box.md), which made staging config an operator-side write.

## Context

The `config` stage writes the staged tree into `/etc/smith/`: the blueprint document, the staged env, the staged resolution and the placement bytes. Until this decision, local smith did it with `sudo` shell one-liners built on the operator's machine and run one at a time over SSH ([#409](https://github.com/byranZA/smith/issues/409)). That was a second remote protocol beside the shipped scripts, with two faults. Its probes ended in `|| true`, so a failed `sudo`, `sha256sum` or `find` read as "nothing there yet", and a prune computed against that empty listing pruned nothing. Its writes ran as a loop, so a failure partway left some files written and returned.

## Decision

**Local smith still loads, plans and resolves the tree, then relays it in one call to a hidden on-box verb, `smith machine stage-config`, which applies it natively in Go.** The secret sources live on the operator's machine, so resolving stays there. Applying moves to the box, where ADR-0008 puts every other implementation.

- **One relayed call carries the whole tree.** The resolved tree travels as a single `tar` stream on SSH stdin: a directory entry per staged directory, and a file entry per staged file with its bytes, mode and owner. Argv carries only the verb and the relay's flags, never a byte of a file. The stream is transport and is never written to the box, so ADR-0006's "no second on-box schema" holds. Every file is sent on every run, and the box compares digests itself, so no probe round-trip comes first.
- **The version gate makes the format free to change.** The call goes through the relay with `--relayed-from`, and `install` runs before `config`, so the two sides run one version by construction. The format carries no compatibility promise.
- **The verb runs as root.** It is invoked as `sudo -n /usr/local/bin/smith …`, the first smith verb to run that way, so writes, modes, owners, renames and deletes are Go calls rather than shell commands. `-n` makes a missing sudo fail at once instead of waiting for a password. The verb touches nothing outside `/etc/smith/`.
- **The box checks the whole stream before it writes anything.** A path outside `/etc/smith/`, a `..` element, or a symlink or hardlink entry refuses the run. The box never trusts the operator's paths because the two sides ought to agree.
- **Absent is a stat result, not missing output.** A destination that does not exist is `staged`. Any other failure to read it is an error, never "absent". A failed walk of the placements directory is an error, never an empty listing.
- **Each file is atomic, the tree is not.** Each file is written to a temporary path beside its destination, given its mode and owner there, and renamed into place, as before. The run stops at the first failure, names that path and exits non-zero. Prune runs only after every write has succeeded. After a failure, each file holds wholly old or wholly new bytes and nothing has been pruned. A re-run converges the rest.
- **The box reports, the relay streams.** The verb prints one report line per entry as it happens, and local smith passes it through, as the workspace stage does. A failure leaves the lines for what already landed, then the error naming the path that did not.

## Considered options

- **Fix the shell recipes in place.** Encode absence explicitly and keep the laptop-built commands. Rejected: it mends the probe fault but keeps a second remote protocol, and a shell one-liner is where the next `|| true` comes from.
- **One apply script owned by the staging package, or a new subcommand of `bootstrap.sh`.** Either one makes the apply a single remote call. Rejected because both keep the apply in shell when the box already runs a smith binary of the same version, and the second ties staging to the script that provisions the base layer.
- **A whole-tree swap.** Write everything to temporary paths, then move all of it into place. Rejected: a directory exchange covers only the placements directory and not the document, env and resolution beside it, and rebuilding the tree rewrites unchanged files, which breaks "unchanged moves no modification time". A failed stage already exits partial and skips `workspace`.
- **A non-root verb that calls `sudo` for each write.** Rejected: it moves the shell recipes into Go rather than removing them.
- **The box plans the tree again from the verbatim document.** Rejected: the version gate means both sides run the same `Plan`, so planning twice adds nothing, and the box still has to check every path itself.

## Consequences

- A dev build of local smith can no longer stage config, because the box refuses to relay from it. `machine setup` from a dev build already failed at `workspace` for the same reason, so it now fails one stage earlier.
- The relay gains a way to invoke on-box smith through `sudo`, and a way to feed it stdin.
- The apply is tested against a real temporary directory, with the `/etc/smith` root and the ownership step injected so the tests run unprivileged. The operator side is tested with a fake connection: one call, no secret in its argv, and stdin that decodes back to the tree.
- A loop on the box that reads placements during an apply can see some files new and some old, never a partial file.
- The format already carries directories, so directory placements ([#307](https://github.com/byranZA/smith/issues/307)) need no new format.
