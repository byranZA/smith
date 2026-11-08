The read verbs print their answer as one JSON document under `--json`, for a program rather than a person. The document is the contract; the prose beside it may change.

Rules for every document:

- Stdout carries exactly one JSON object followed by a newline. Warnings, hints and refusals go to stderr.
- A verb that produced its answer prints its document whatever the verdict. A verb that could not produce one prints nothing on stdout and says why on stderr. The exit code is the same with or without `--json`.
- Keys are snake_case. Timestamps are RFC 3339 UTC. An absent optional value is left out.
- Every document has an integer `schema_version`, starting at 1 and versioned per document.

Change policy, before v1: a document may gain fields at any time, so ignore keys you do not know. Renaming or removing a field is a breaking change: it bumps `schema_version` and the release notes announce it.

The documents:

`smith version`
  schema_version  1
  version         the smith version, or "dev" for a development build
  commit          the VCS commit, when the binary carries it
  built_at        the build time, when the binary carries it

`smith machine list`
  schema_version  1
  boxes           the inventory in name order, [] when empty; each box has:
    name          the box's name
    target        the SSH target registered for it
    reachable     true or false whether the box answered; present only under --probe

`smith machine status`
  schema_version  1
  box             the name or target as given
  verdict         matches, drifted, partial, never_set_up or unreachable; every verdict prints
                  a document, and the exit code is the same as without --json
  access_mode     public or tailscale, when the box has a marker
  findings        [] unless the box matches or has drifted; each finding has:
    phase         the setup phase that owns the fact
    fact          the fact's name
    expected      the value setup established
    actual        the probed value
    status        match, drift, unknown or unverifiable
  missing_phases  the phases setup has not recorded; present only when partial
  error           why smith could not connect; present only when unreachable
  note            the marker's schema-skew note, when there is one
  memory_kib      the box's RAM; absent when unreadable
  swap_kib        the box's swap, 0 for none; absent when unreadable
  docker          present unless unreachable; kept apart from the findings:
    ports         each published container binding, with exposed: true when it is beyond loopback
    unreadable    true when the bindings could not be checked
