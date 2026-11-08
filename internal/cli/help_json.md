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
