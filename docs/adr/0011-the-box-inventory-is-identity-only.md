# The box inventory is identity only — and the `@` decides

## Context

Every smith verb takes an address, and until this decision the operator was the only thing that
remembered it. Setup computes the address that will work from now on, prints it once, and forgets
it; what the operator types tomorrow is the address they set the box up with, which by then is
firewalled off or refuses root. So smith gained the **box inventory** — `~/.smith/cache/boxes.json`,
a name → target address book that [ADR-0006](./0006-the-blueprint-config-surface.md) had already
placed as the one derived thing in the config home.

The pressure this ADR exists to hold is what the inventory grows into. It sits on the operator's
machine, it is the only file smith owns that spans boxes, and every later slice will have a fact it
would be *convenient* to keep there — the access mode, so `status` need not connect; the blueprint,
so a destroyed box's credentials are still knowable; a `last_seen`, so `list` can grey out what has
not answered lately. Each is individually reasonable. Together they turn an address book into a
second state store, standing beside the marker on the box with no rule for which wins.

The second pressure is the resolution rule. A name and an SSH target are both strings, and the
obvious ways to tell them apart — a `--name` flag, a sigil, a per-verb convention — all buy
disambiguation by making the operator carry it.

Both were settled while resolving [Grill: local box-inventory cache](https://github.com/byranZA/smith/issues/65).
This ADR records them so that widening either is a re-opening rather than a refinement.

## Decision

**The inventory stores identity and nothing else — a name and one opaque target — and the `@`
decides how any value the operator types is read.**

### An entry is two fields of identity

```json
{
  "schema_version": 1,
  "boxes": {
    "dev":     { "target": "smith@100.92.14.7" },
    "scratch": { "target": "smith@203.0.113.42" }
  }
}
```

The name is the key and is *also* on the marker; the target is the opaque string smith proved works
at the end of setup. **The access mode, the completed phases, the blueprint pointer and the provider
destroy reference all live on the marker, on the box — one copy each, no second answer.**

This is not a minimalism preference, it is this repo's spine restated for a new file. The marker is
a ledger, not a gate. Session running-state is a live `tmux has-session` probe with no daemon.
`status` reports drift and never remediates ([ADR-0010](./0010-session-teardown-safety-and-the-work-state-readout.md)
extends the same posture to teardown: `list` displays a predicate, `rm` gates on it, nothing is
tracked between). The moment the inventory holds a second copy of a fact the box already knows,
smith can answer one question two ways and has no rule for picking — and the wrong copy is the one
that costs nothing to keep and everything to trust.

### Staleness is dissolved, not managed

The grill asked for a staleness and refresh policy. Identity-only means there is no such question to
answer: **no TTL, no refresh pass, no `last_seen`.**

The asymmetry is the whole argument. A wrong *address* fails loudly on connect, at the moment it is
wrong, in a message that names what smith tried. A wrong cached *access mode* fails quietly, as a
report about a box that is fine, or a decision to skip a step that was needed. Only facts of the
second kind need a freshness policy, and the inventory holds none of them.

`machine list --probe` is the shape this permits: it fans out over SSH and flags unreachable boxes
inline **without writing anything back**. Reach is displayed, never stored. Nothing is auto-pruned
either — a failed connect means rebooting, off-tailnet or firewalled at least as often as it means
destroyed, and with no reconstruction path (below) an auto-pruned entry is genuinely gone.

### The `@` decides — one rule, every verb

- **Contains `@`** → a literal SSH target, used as written, **never** looked up.
- **No `@`** → looked up in the inventory; on a miss, handed to `ssh` verbatim.

No flag, no sigil, no per-verb convention. The literal branch keeps
`machine setup root@203.0.113.10` working for a box smith has never seen. The miss branch is what
makes an `ssh_config` alias or a MagicDNS name a usable smith argument with **no smith configuration
at all**, because the target is never parsed — smith passes it to system `ssh`/`scp` as given
([`connection.go:74`](../../internal/connection/connection.go:74)), which is *reference, not value*
([ADR-0002](./0002-secrets-are-references-not-values.md)) extended from secrets to reach.

Two costs are accepted rather than mitigated:

**An inventory name shadows an identically-named `ssh_config` alias.** Inventory-wins is one
sentence to state and never surprises twice; the alternative is a sigil the operator types forever
to resolve a collision they will mostly never have. The shadowed file is the operator's own, and the
name that wins is the one they chose for the box.

**`machine status <ip>` changed meaning for boxes provisioned by v0.1.0.** A bare address used to
imply the `smith` login; under one rule it is a bare value, so it goes to `ssh` as written and
connects as the local user. This is paid for **in the error message, not by giving `status` a
private convention** — a failed connect on an unrecognised bare value names both fixes, the address
that would have worked and the command that registers the box so the name works from now on:

```
203.0.113.10 is not a registered box, so smith handed it to ssh as written.

  Try the address:  smith@203.0.113.10
  Or register it:   smith machine add smith@203.0.113.10
```

A per-verb exception would have bought the old spelling at the price of the property that makes the
rule worth having: that `setup`, `status`, `list --probe` and every session verb read their argument
identically.

### v1 has no automatic reconstruction path

Stated plainly rather than implied. Rebuilding an entry means reading a box's marker, which means
already knowing that box's address — so reconstruction cannot bootstrap itself. Provider-side
discovery, which would close the gap for provider-created boxes, is deferred to
[#73](https://github.com/byranZA/smith/issues/73); a brought-your-own box is enumerated by nothing
even then.

So a deleted `boxes.json` is rebuilt **by hand, one `machine add` per box**, from the provider
dashboard or shell history. The inventory is **rebuildable, not self-rebuilding**, and
[ADR-0006](./0006-the-blueprint-config-surface.md)'s "derived, never authored" must be read that
way — losing the file loses nothing unique, but smith cannot fetch it back for you. What makes the
manual path cheap rather than lossy is the marker recording the box's operator-chosen name: without
it a re-added box comes back as `203.0.113.10` and every command in shell history that says `dev`
breaks.

## Considered options

- **Caching the access mode** — rejected, and the representative case for every field like it. It
  would let `status` answer without connecting, which is exactly the answer that must not be given
  from a cache: a box's door is the thing most likely to have changed since smith last looked.
- **Recording the blueprint per box** — rejected here despite a real consumer. Knowing what a
  *destroyed* box held is the one gap in
  [ADR-0009](./0009-provisioned-secrets-sit-in-plaintext.md)'s revocation story, and an inventory
  copy would close it for free. It is still a second copy of a marker field, and buying one
  after-the-fact convenience with a permanent two-answers-one-question is the trade this ADR
  refuses. The gap stays open and belongs to whoever ships box `destroy`.
- **A `last_seen` timestamp, a TTL, or a refresh pass** — rejected. All three are machinery for
  keeping stored facts honest, and there are no stored facts to keep honest. A timestamp would also
  read as authority it does not have: last-seen-recently says nothing about now.
- **Auto-pruning entries that fail to answer** — rejected. Unreachable is not destroyed, and with no
  reconstruction path the deletion is unrecoverable in a way the failed probe never justified.
- **Per-box files instead of one index** — rejected. Entries are two fields, there is no
  concurrent-writer story worth engineering for, and one file is one thing to `cat`, hand-edit or
  delete.
- **A `--name` flag, or a sigil, to disambiguate name from target** — rejected. It taxes every
  invocation to resolve an ambiguity the `@` already resolves for free, and a rule the operator must
  remember to invoke is a rule that is wrong whenever they forget.
- **Letting `machine status <ip>` keep implying the `smith` login** — rejected. One verb reading its
  argument differently from the rest is the whole cost of "one rule, every verb", for the sake of a
  spelling one error message can repair.
- **Provider-side discovery as the reconstruction path** — deferred, not rejected; see
  [#73](https://github.com/byranZA/smith/issues/73).

## Consequences

- **The inventory verbs are the exception to "every verb runs on the box."** A box knows of no other
  boxes, so `list`, `add` and `forget` have nothing to relay to and are local-only; on-box smith
  carries no `~/.smith/` and no inventory at all
  ([ADR-0008](./0008-smith-runs-on-the-box.md), [ADR-0006](./0006-the-blueprint-config-surface.md)).
  A contributor's instinct after reading the relay decision is to make `list` relay somewhere;
  it must not.
- **Registration is read-only towards the box.** `add` reads a marker and registers, or refuses with
  `run smith machine setup <target> first`. Writing a marker onto an unprovisioned box would claim
  provisioned/secured/reachable state that does not exist, and only `setup` mutates — the
  setup/status seam holds.
- **The stored target is one smith proved**, never one it derived. Lock-out safety already forces a
  probe over the new door before the old one closes, so at the end of setup smith holds a working
  address for free. The inventory is therefore structurally incapable of holding an address that
  never worked, and a wrong MagicDNS name ([#74](https://github.com/byranZA/smith/issues/74)) cannot
  reach it.
- **The marker is now the only home for per-box facts**, which raises the bar on marker schema
  changes rather than lowering it: a fact that belongs to a box and is not on the marker has nowhere
  else to live.
- **`schema_version` is honoured the way the marker's is** — read what you understand, and on a
  *newer* version refuse to write rather than silently rewriting a file a newer smith owns
  ([`marker.go:78`](../../internal/marker/marker.go:78) is the posture copied). Two builds touch this
  file the moment you upgrade, or downgrade to debug.
- **Anyone adding a third field re-opens this decision.** The test is not "is this field useful" —
  they all are — but "does the box already know it?" If it does, the field belongs on the marker and
  the inventory stays an address book.

See [issue #65](https://github.com/byranZA/smith/issues/65) and
[issue #92](https://github.com/byranZA/smith/issues/92).
