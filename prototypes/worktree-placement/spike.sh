#!/usr/bin/env bash
# PROTOTYPE — worktree + env/seed placement spike for smith (issue #57). THROWAWAY.
#
# Answers one question: does "clone once, cut N worktrees, materialise files into
# each" actually work — and is the placement PRIMITIVE right?
#
#   PLACEMENT PRIMITIVE (the thing this spike exists to lock):
#     source-reference  →  destination-path   [ mode: converge | once ]
#
#   - source-reference : a scheme:arg string, REFERENCE NOT VALUE, exactly like
#                        internal/secret (file:/path, env:VAR). The blueprint
#                        points AT the content; it never embeds it.
#   - destination-path : a path relative to each worktree root.
#   - mode:
#       converge  overwrite on every run  → desired state wins (the .env case)
#       once      write only if absent    → local edits survive (the seed case)
#
# The money shot is the SECOND pass: between passes we change the env source AND
# hand-edit a seed file inside a worktree, then re-run. converge must overwrite
# the env everywhere; once must leave the hand-edited seed untouched.
#
#   run     full guided demonstration end-to-end (setup → pass 1 → mutate → pass 2)
#   status  show the worktree tree + placed-file contents
#   free    re-run the placement pass only (edit files yourself between runs)
#   down    delete the scratch world
#
# Everything lives under ./.run (gitignored). No provider, no network — the
# "upstream repo" is created locally so the spike runs offline.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN="$HERE/.run"
UPSTREAM="$RUN/upstream.git"        # the "remote" repo smith would clone
WORKSPACE="$RUN/workspace"          # the on-box clone (one per managed repo)
TREES="$RUN/worktrees"             # where cut worktrees live
BLUEPRINT="$RUN/blueprint"          # the reference sources the blueprint points AT
N=3                                 # how many worktrees to cut

c_reset=$'\033[0m'; c_dim=$'\033[2m'; c_grn=$'\033[32m'; c_yel=$'\033[33m'
c_cyn=$'\033[36m'; c_red=$'\033[31m'; c_bold=$'\033[1m'
say()  { printf '%s\n' "$*"; }
head() { printf '\n%s──▶ %s%s\n' "$c_bold" "$*" "$c_reset"; }
note() { printf '%s   %s%s\n' "$c_dim" "$*" "$c_reset"; }

# ── the placement primitive ────────────────────────────────────────────────
# resolve_ref  scheme:arg  → prints the resolved SOURCE PATH (reference→value).
# We resolve to a *path* (not inlined bytes) so the copy is a normal file op;
# env: writes the var's value to a temp file so both schemes feed one code path.
resolve_ref() {
  local ref="$1" scheme arg
  scheme="${ref%%:*}"; arg="${ref#*:}"
  [ "$scheme" != "$ref" ] || { say "${c_red}bare literal refused: $ref (use file: or env:)${c_reset}" >&2; exit 1; }
  case "$scheme" in
    file) [ -f "$arg" ] || { say "${c_red}file ref not found: $arg${c_reset}" >&2; exit 1; }
          printf '%s' "$arg" ;;
    env)  local tmp; tmp="$(mktemp)"; printf '%s' "${!arg-}" > "$tmp"; printf '%s' "$tmp" ;;
    *)    say "${c_red}unknown scheme: $scheme${c_reset}" >&2; exit 1 ;;
  esac
}

# place  REF  DEST  MODE  MODEBITS  WT
#   the single primitive every blueprint placement flows through.
place() {
  local ref="$1" dest="$2" mode="$3" bits="$4" wt="$5"
  local src target; src="$(resolve_ref "$ref")"; target="$wt/$dest"
  mkdir -p "$(dirname "$target")"
  case "$mode" in
    converge)
      if [ -f "$target" ] && cmp -s "$src" "$target"; then
        note "= converge  $dest  ${c_dim}(matches ref, no-op)"
      else
        local verb="wrote"; [ -f "$target" ] && verb="OVERWROTE"
        install -m "$bits" "$src" "$target"
        printf '   %s↻ converge  %s%s  %s(%s from %s)%s\n' "$c_yel" "$dest" "$c_reset" "$c_dim" "$verb" "$ref" "$c_reset"
      fi ;;
    once)
      if [ -e "$target" ]; then
        printf '   %s✓ once      %s%s  %s(present — preserved, NOT clobbered)%s\n' "$c_grn" "$dest" "$c_reset" "$c_dim" "$c_reset"
      else
        install -m "$bits" "$src" "$target"
        printf '   %s+ once      %s%s  %s(seeded from %s)%s\n' "$c_cyn" "$dest" "$c_reset" "$c_dim" "$ref" "$c_reset"
      fi ;;
    *) say "${c_red}unknown mode: $mode${c_reset}" >&2; exit 1 ;;
  esac
}

# The blueprint's placement list. In smith this is declared config; here it's a
# function so the spike stays one file. Each line IS the primitive.
materialise() {
  local wt="$1"
  place "file:$BLUEPRINT/env.reference"  ".env"     converge 600 "$wt"
  place "env:SMITH_EXTRA_ENV"            ".env.ci"  converge 600 "$wt"
  place "file:$BLUEPRINT/seed.sql"       "seed.sql" once     644 "$wt"
}

# ── worktree plumbing ──────────────────────────────────────────────────────
setup_world() {
  rm -rf "$RUN"; mkdir -p "$RUN" "$BLUEPRINT" "$TREES"

  head "Blueprint sources (what the blueprint points AT — references, not values)"
  cat > "$BLUEPRINT/env.reference" <<'EOF'
APP_ENV=dev
DATABASE_URL=postgres://localhost/app
FEATURE_FLAG=off
EOF
  cat > "$BLUEPRINT/seed.sql" <<'EOF'
-- seed data: written ONCE, then it's the worktree's own to mutate
INSERT INTO users (name) VALUES ('alice');
EOF
  note "file:$BLUEPRINT/env.reference   → .env      (converge)"
  note "env:SMITH_EXTRA_ENV             → .env.ci   (converge)"
  note "file:$BLUEPRINT/seed.sql        → seed.sql  (once)"

  head "Clone the repo ONCE (the managed workspace)"
  git init -q -b main "$UPSTREAM"
  git -C "$UPSTREAM" -c user.email=s@s -c user.name=s commit -q --allow-empty -m "root"
  printf 'app source\n' > "$UPSTREAM/README.md"
  git -C "$UPSTREAM" add -A && git -C "$UPSTREAM" -c user.email=s@s -c user.name=s commit -q -m "app"
  git clone -q "$UPSTREAM" "$WORKSPACE"
  note "cloned $UPSTREAM → $WORKSPACE"

  head "Cut $N worktrees (one per session, side-by-side branches)"
  for i in $(seq 1 "$N"); do
    git -C "$WORKSPACE" worktree add -q -b "session-$i" "$TREES/session-$i" >/dev/null
    note "worktree session-$i  →  $TREES/session-$i"
  done
}

pass() {
  local label="$1"
  head "$label — run the placement pass over every worktree"
  for i in $(seq 1 "$N"); do
    printf '  %s%s%s\n' "$c_bold" "session-$i" "$c_reset"
    materialise "$TREES/session-$i"
  done
}

run_in_each() {
  head "Run something in each worktree (prove worktrees just work)"
  for i in $(seq 1 "$N"); do
    local wt="$TREES/session-$i"
    # read the placed env back — proves it's a usable file, not just present
    local app_env; app_env="$(bash -c "set -a; . '$wt/.env'; printf '%s' \"\$APP_ENV\"")"
    local branch; branch="$(git -C "$wt" branch --show-current)"
    printf '  %ssession-%s%s  on branch %s%s%s  .env→APP_ENV=%s%s%s  files: %s\n' \
      "$c_bold" "$i" "$c_reset" "$c_cyn" "$branch" "$c_reset" \
      "$c_grn" "$app_env" "$c_reset" "$(cd "$wt" && ls -1 | tr '\n' ' ')"
  done
  # prove isolation: a commit in session-1 is invisible to session-2's branch
  echo "work in progress" > "$TREES/session-1/WIP.txt"
  git -C "$TREES/session-1" add -A && git -C "$TREES/session-1" -c user.email=s@s -c user.name=s commit -q -m "wip in session-1"
  note "committed WIP.txt on session-1 → session-2 sees it? $([ -e "$TREES/session-2/WIP.txt" ] && echo YES || echo no) (worktrees are isolated)"
}

# ── commands ───────────────────────────────────────────────────────────────
cmd_run() {
  setup_world
  export SMITH_EXTRA_ENV=$'CI=true\nTOKEN=ref-not-value-1'
  pass "PASS 1 (fresh worktrees)"
  run_in_each

  head "MUTATE between passes (this is the whole point)"
  note "1. change the env SOURCE:      FEATURE_FLAG off → on, and SMITH_EXTRA_ENV token rotates"
  sed -i.bak 's/FEATURE_FLAG=off/FEATURE_FLAG=on/' "$BLUEPRINT/env.reference"; rm -f "$BLUEPRINT/env.reference.bak"
  export SMITH_EXTRA_ENV=$'CI=true\nTOKEN=ref-not-value-2-ROTATED'
  note "2. hand-edit seed.sql INSIDE session-2 (as an agent/human would)"
  printf "INSERT INTO users (name) VALUES ('bob-added-by-hand');\n" >> "$TREES/session-2/seed.sql"

  export SMITH_EXTRA_ENV
  pass "PASS 2 (re-run — converge overwrites, once preserves)"

  head "Verdict"
  local flag; flag="$(grep FEATURE_FLAG "$TREES/session-1/.env")"
  local tok;  tok="$(grep TOKEN "$TREES/session-1/.env.ci")"
  if [ "$flag" = "FEATURE_FLAG=on" ] && [[ "$tok" == *ROTATED* ]]; then
    say "  ${c_grn}✓ converge${c_reset} .env & .env.ci re-materialised from the changed sources ($flag / $tok)"
  else
    say "  ${c_red}✗ converge FAILED${c_reset} ($flag / $tok)"
  fi
  if grep -q 'bob-added-by-hand' "$TREES/session-2/seed.sql"; then
    say "  ${c_grn}✓ once${c_reset}     session-2 seed.sql kept its hand-edit (bob-added-by-hand survived)"
  else
    say "  ${c_red}✗ once FAILED${c_reset} — hand-edit was clobbered"
  fi
  note "perms: .env is $(stat -f '%Sp' "$TREES/session-1/.env" 2>/dev/null || stat -c '%A' "$TREES/session-1/.env") (600, secret-ish); seed.sql is $(stat -f '%Sp' "$TREES/session-1/seed.sql" 2>/dev/null || stat -c '%A' "$TREES/session-1/seed.sql")"
  echo; note "inspect with: $0 status    tear down with: $0 down"
}

cmd_status() {
  [ -d "$TREES" ] || { say "no world yet — run: $0 run"; exit 0; }
  head "worktrees"; git -C "$WORKSPACE" worktree list
  for d in "$TREES"/session-*; do
    printf '\n%s%s%s\n' "$c_bold" "$d" "$c_reset"
    for f in .env .env.ci seed.sql; do
      [ -f "$d/$f" ] && { printf '%s  %s:%s\n' "$c_cyn" "$f" "$c_reset"; sed 's/^/    /' "$d/$f"; }
    done
  done
}

cmd_free() { [ -d "$TREES" ] || { say "no world — run: $0 run first"; exit 1; }; pass "FREE re-run"; }
cmd_down() { rm -rf "$RUN"; say "world removed."; }

case "${1:-run}" in
  run)    cmd_run ;;
  status) cmd_status ;;
  free)   cmd_free ;;
  down)   cmd_down ;;
  *)      say "usage: $0 {run|status|free|down}"; exit 1 ;;
esac
