# The box inventory

Every smith verb takes an address, and IPs are not memorable. The **box
inventory** is smith's answer: a name → address book at
`~/.smith/cache/boxes.json`, written when a setup succeeds and read by every
verb that takes a box.

```json
{
  "schema_version": 1,
  "boxes": {
    "dev":     { "target": "smith@100.92.14.7" },
    "scratch": { "target": "smith@203.0.113.42" }
  }
}
```

That is the whole file. An entry is a name and one opaque SSH target — nothing
else. Access mode, completed phases, the blueprint the box was built from and
the provider destroy reference all live on the box's marker
(`/etc/smith/bootstrap.json`), one copy each. There is no TTL, no refresh pass
and no `last_seen`, because nothing here can silently go stale: a wrong address
fails loudly the moment you connect.

**The target is one smith proved**, unless you name one yourself. Setup stores
the address it has just opened a connection over — not the address you typed,
which hardening kills (root login is closed), and not one smith derived but
never opened. Over tailscale that is the tailnet address the lock-out-safety
probe came in on, so a wrong MagicDNS name cannot reach the file. The one way
an unproved address gets in is [`--target`](#--target-register-an-address-of-your-own),
where you are pointing at your own naming scheme and smith takes you at your
word.

## Naming a box on setup

`--name` gives the box the name you will type from now on:

```sh
smith machine setup root@203.0.113.10 --name dev
```

```
registered smith@203.0.113.10 as "dev"

Reach it by name from now on:
  smith machine status dev
  smith machine setup dev
```

Omit `--name` and smith takes the most specific name it has, in this order:

**`--name` → the name already on the box's marker → the blueprint's name → the
host part of the target.**

The marker sitting above the blueprint is what makes re-setup idempotent: a box
already registered as `dev` stays `dev` however you address it on the re-run,
rather than growing a second entry named after its IP.

A name that already reaches a **different** box is a hard error — never a
silent `dev-2`:

```
the box is provisioned but unregistered: cannot register smith@198.51.100.7: "dev" already names a different box (smith@203.0.113.10): register this one under another name with --name, or forget that one first

Every setup phase completed, so the box is provisioned and secured; smith just
did not write it down. Register it under a name that is free:
  smith machine add smith@198.51.100.7 --name <name>
```

The same name mapping to the same target is a no-op, which is what makes
re-running setup safe. Two names for one box are legal; the file has no
uniqueness rule on targets.

Passing a `--name` that differs from the name on the box's marker is a
**rename**: the marker is rewritten, the old entry goes, the new one is
written, and smith says so.

```sh
smith machine setup dev --name staging
```

```
renamed "dev" to "staging"
registered smith@203.0.113.10 as "staging"

Reach it by name from now on:
  smith machine status staging
  smith machine setup staging
```

### `--target`: register an address of your own

`--target` stores an address verbatim instead of the one smith proved. Use it
when you have your own naming scheme — an `ssh_config` alias, a MagicDNS name,
a bastion hop:

```sh
smith machine setup root@203.0.113.10 --name dev --target dev.internal
```

smith does not probe a `--target`: you are pointing at something it cannot
meaningfully verify and should not second-guess.

### When registration does not happen

A setup whose phases all completed but whose box will not answer as the `smith`
user is **provisioned but unregistered**. The phases genuinely finished, so
this is a partial outcome, not a failure — and the fix is a command, not a
re-run:

```
the box is provisioned but unregistered: smith could not reach the box as smith@203.0.113.10

Every setup phase completed, so the box is provisioned and secured; smith just
has no name it can reach it by. Register it once it answers:
  smith machine add smith@203.0.113.10
```

A setup that *fails* registers nothing at all.

## The `@` decides

One rule governs the argument to every verb that takes a box:

- A value **containing `@`** is a literal SSH target. It is used exactly as
  written and never looked up — so `smith machine setup root@203.0.113.10`
  works on a box smith has never seen.
- A value **without `@`** is looked up in the inventory. On a hit you get the
  registered target; on a **miss the value is handed to `ssh` as written**.

No flag, no sigil, no configuration.

The miss case is the useful half: `ssh` resolves what it is given against
`ssh_config` and DNS, so an alias in your `~/.ssh/config` and a MagicDNS name
both work as smith arguments with **no smith configuration at all** —

```sh
smith machine status dev-box       # a Host entry in ~/.ssh/config
smith machine status smith-web-1   # a MagicDNS name
```

— and a name in the inventory shadows an identically-named `ssh_config` alias,
so the name you chose is the one that wins.

The cost of one rule is that `smith machine status 203.0.113.10` no longer
quietly means `smith@203.0.113.10`; it is an address handed to `ssh`, which
connects as your local user. When such a connection fails, smith names both
fixes:

```
203.0.113.10 is not a registered box, so smith handed it to ssh as written.

  Try the address:  smith@203.0.113.10
  Or register it:   smith machine add smith@203.0.113.10
```

`setup` is the one verb that also needs the target's shape — its firewall and
tailnet steps are written in terms of the host — so it still requires
`<login>@<host>` once the argument has resolved.

## Listing boxes

`machine list` is instant and offline. It opens no connection, runs nothing on
any box, and creates no file — a box knows of no other boxes.

```sh
smith machine list
```

```
NAME     TARGET
dev      smith@100.92.14.7
scratch  smith@203.0.113.42

2 boxes
```

Boxes come out in name order, and the count line is always printed. An empty
or missing inventory is not an error:

```
no boxes registered

Register one with:
  smith machine add <login>@<host>
```

Add `--probe` to connect to each box and report whether it answers:

```sh
smith machine list --probe
```

```
NAME     TARGET              REACHABLE
dev      smith@100.92.14.7   reachable
scratch  smith@203.0.113.42  unreachable
```

Reachability is a display concern, never a stored one: probing writes nothing,
prunes no entry, and an unreachable box does not fail the command.

## Registering a box smith already provisioned

`machine add` registers a box that already carries a marker — the way you
rebuild a deleted inventory, and the way a second laptop learns the boxes the
first one set up.

```sh
smith machine add smith@100.92.14.7             # named by the box's marker
smith machine add smith@100.92.14.7 --name api  # named by you
```

```
registered smith@100.92.14.7 as "dev"
built from the blueprint "acme"
```

The name comes from `--name`, else the name on the box's marker, else the
target's host. A marker written before smith recorded names says so:

```
the box's marker records no name, so smith named it after its host.
Register it under another name with: smith machine add smith@203.0.113.10 --name <name>
```

**`add` never writes a marker.** It connects, reads, and registers or refuses.
A box carrying no marker is not an unregistered smith box — it is a box smith
has never provisioned, and stamping one would claim provisioned, secured and
reachable state the box does not have:

```
add refused: smith@198.51.100.7 has never been set up by smith: it carries no marker at /etc/smith/bootstrap.json.
Provision it first with:
  smith machine setup <login>@198.51.100.7
```

A box that will not answer is refused the same way. Re-adding a box already
registered at the same target succeeds unchanged; adding under a name that
reaches a different box is the same hard error setup gives.

## Forgetting a box

`machine forget` is the only removal, and it removes nothing but a line in a
file. No connection is opened, the box keeps running, and you can add it back
whenever you want it:

```sh
smith machine forget dev
```

```
forgot "dev" (smith@100.92.14.7)

Register it again with:
  smith machine add smith@100.92.14.7 --name dev
```

Forgetting a name no box is registered under is an error, not a silent
success — a mistyped name should tell you the entry you meant is still there.

## Rebuildable, not self-rebuilding

`boxes.json` holds nothing that exists only there: every fact about a box lives
on the box's marker, and a name you lose can be re-chosen. So deleting the file
loses nothing permanent — **but smith cannot fetch it back for you.**

Rebuilding an entry means SSHing to a box, which means already knowing its
address. Provider-side discovery is not in v1, so a deleted `boxes.json` is
rebuilt **by hand, one `machine add` per box**, with addresses out of your
provider dashboard or your shell history:

```sh
smith machine add smith@100.92.14.7
smith machine add smith@203.0.113.42
smith machine list
```

Each box comes back under the name its marker records, so `dev` is `dev` again
and every command in your shell history keeps working. That is what the
marker's name field buys, and it is why re-adding is cheap rather than
automatic.

Treat the file as worth keeping — it is machine-local and gitignored, so it is
not in the repo you commit your config home to.

## The file itself

`~/.smith/cache/boxes.json`, created by the first registration, along with
`~/.smith/` and `~/.smith/cache/` at `0700` and a `.gitignore` listing
`cache/`. An existing `.gitignore` is **appended to**, never rewritten: the
config home is yours. Reading never creates anything.

Writes go through a temp file and a rename, so an interrupted write cannot
truncate the file.

The file carries a `schema_version`. A file written by a **newer** smith is
still read — you get the entries this build recognises, plus a note — but never
written to:

```
smith machine forget dev
```

```
forget dev: refusing to write /home/ada/.smith/cache/boxes.json: the inventory was written by a newer smith: upgrade smith, or the entries it holds would be lost
```

A file that is not valid JSON is reported with its path and refused, rather
than overwritten:

```
inventory /home/ada/.smith/cache/boxes.json: decode inventory: invalid character 'o' in literal null (expecting 'u')
```
