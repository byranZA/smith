# smith updates itself, then offers to converge every box

> Amends [ADR-0008](./0008-smith-runs-on-the-box.md), which ruled local self-upgrade out of scope.

## Context

ADR-0008 left updating local smith to the operator: `curl` a new release, then run `smith machine upgrade` once per box. Because on-box smith refuses a version mismatch, every box stays unusable until it is converged, so a local update always comes with that per-box chore. Releases are now frequent enough that the manual path is the common one.

## Decision

**`smith update` replaces local smith with a published release, then offers to converge every inventoried box to it.**

- **The release comes from GitHub**: the latest release, or the tag `--version` names, including an older one. The archive is checked against the release's `checksums.txt` before anything is written. This is local smith's only network call to GitHub, and it sits behind a single seam (`internal/selfupdate`).
- **Install is write-beside-and-rename** over the running binary's real path, so a symlink is kept and a failure leaves the old binary untouched. It never uses sudo: a directory the user cannot write is refused.
- **Boxes converge to the new local version** the way `machine upgrade` does, after one question for all of them, or none with `--boxes`. An unattended run upgrades no box and prints the commands. One unreachable box does not stop the others, but it does make the exit non-zero.
- **A release older than `smith update` cannot run the box step.** smith probes the new binary and, if the release predates `smith update`, prints the per-box commands instead of switching to it.
- **It refuses on Windows**, where a running binary cannot be replaced, and **on a box**, whose smith version is driven from the operator's machine.

Auto-update stays out of scope, for the reasons ADR-0008 gives: smith updates only when you run `smith update`.

## Considered options

- **Keep the manual path.** Rejected: the per-box chore follows every update, and the version gate turns forgetting it into a refusal.
- **Update local smith only.** Rejected: on-box smith refuses a mismatch, so a local update that leaves the boxes behind leaves them unusable.
- **A hosted `install.sh`.** Rejected: it serves the first install, not the update-then-converge loop, and adds a script to host and trust.
