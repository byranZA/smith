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

`smith session list`
  schema_version  1
  sessions        the box's sessions in repo then branch order, [] when none; each has:
    name          the name the other session verbs take
    repo          the repo the worktree is cut from
    branch        the branch checked out; absent for a detached worktree
    worktree      the absolute path of the checkout on the box
    live          true when its tmux session is running
    dirty         true when the worktree holds uncommitted work
    unpushed      the commits on the branch no remote has
    driver        human or loop
    outcome       how a loop-driven run ended; present only when one was recorded
  A relayed run prints the box's own stdout byte for byte. A version-skew refusal prints nothing on stdout.

`smith loop list`
  schema_version  1
  spec            the number of the spec, or of the task run on its own
  work_branch     the work branch a run would use
  next            the number of the first available task, or null when there is none
  tasks           every task in the spec's order, each with:
    number        the task's issue number
    title         the task's title
    labels        the names of its labels, [] when none
    state         available, blocked, human, unready or closed
    blocked_by    the numbers of its open blockers, [] when none
  Label warnings go to stderr. An issue that cannot be read prints nothing on stdout.

`smith blueprint check`
  schema_version  1
  preferences     the preferences file: path, and found (false when there is none)
  blueprint       the blueprint, present only when one was named: name as given, and path
  resolved        the configuration setup would use; absent when there are problems. Each of
                  access, terminal, workspace is {value, origin}. Also present when declared:
    provider      {value, origin}, value being the provider command; replaces_preference: true
                  when a blueprint's block replaces the preferences' whole block
    git           user_name and user_email, each {value, origin}
    repos         each repo has name and url as {value, origin}, and when declared base
                  ({value, origin}), tools and env (each entry {value, origin}) and placements
    packages, groups   lists of {value, origin}, one per entry
    tools, env    maps from name to {value, origin}
    placements    each has from, to, origin, and when declared mode and perms
  problems        [] when the files are valid; each has message, and line and path when known
  origin is one of flag, blueprint, task_label, repo_file, preferences, default.
  Problems print in the document and the exit code stays 1. A blueprint that cannot be found
  prints nothing on stdout.
