#!/usr/bin/env python3
"""Ralph loop — GitHub-issue edition.

Drives the intraday-breakout autonomous loop against a spec (PRD) GitHub issue
and its child task issues, the way `/spec-to-tasks` and `/review-and-task`
produce them:

  - Children are GitHub **sub-issues** of the spec (`gh issue create --parent`),
    and their dependencies are native **blocked-by** links (`--blocked-by`).
  - AFK tasks are labelled `ready-for-agent`; HITL tasks `ready-for-human`.

Older specs used body conventions instead of GitHub's native relationships — a
`## Tasks` checklist on the spec, `Part of #<spec>` in each child, and a
`## Blocked by` section listing `#<n>`. Both are read, so a spec that is half
migrated (native sub-issues filed against a spec that still carries a checklist)
behaves correctly.

Each iteration the driver — not the prompt — decides what happens:

  1. Query GitHub for the spec's children and their current state.
  2. Pick the first task that is OPEN, `ready-for-agent`, and unblocked
     (all its blockers closed). Dependency order comes from the spec's
     `## Tasks` checklist.
  3. Run one headless agent session (Claude Code by default, or Codex via
     `--agent codex`) on exactly that issue, using `ralph/ralph-prompt.md`.
  4. Re-check the issue: closed means done; still open means the agent got
     stuck (it leaves a WIP comment). After `--max-attempts` failed tries the
     task is skipped so the loop doesn't spin on it forever.

The loop exits when no AFK task is available (spec complete, or only
human/blocked/skipped tasks remain) or `--max-iterations` is hit. Because it
re-queries GitHub every iteration, new fix issues filed by `/review-and-task`
mid-run (also children of the spec) are picked up automatically.

Usage:
  python ralph/ralph.py <spec-issue>              # e.g. 42  or  the issue URL

  # Autonomous loop (Claude Code, the default backend)
  python ralph/ralph.py 42                         # loop until no task is available
  python ralph/ralph.py 42 --max-iterations 20     # cap the number of agent runs
  python ralph/ralph.py 42 --max-attempts 3        # retries before a task is skipped
  python ralph/ralph.py 42 --model sonnet          # override the claude model
  python ralph/ralph.py 42 --permission-mode bypassPermissions

  # Autonomous loop (Codex backend)
  python ralph/ralph.py 42 --agent codex           # headless: codex exec --yolo
  python ralph/ralph.py 42 --agent codex --model gpt-5-codex

  # Human-in-the-loop: pick the next available task, run ONE interactive
  # session on it, then exit (successor to the old ralph-once.sh)
  python ralph/ralph.py 42 --once                  # claude, interactive
  python ralph/ralph.py 42 --once --agent codex    # codex, interactive TUI

  # Inspect without running an agent
  python ralph/ralph.py 42 --list                  # show the next available task and exit
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
PROMPT_FILE = HERE / "ralph-prompt.md"
LOG_FILE = HERE / "ralph.log"

READY_LABEL = "ready-for-agent"   # AFK — an agent may pick it up
HUMAN_LABEL = "ready-for-human"   # HITL — needs a person
SPEC_LABEL = "spec"               # a PRD, not a task — loop over it, never run it

# The coding agent that does the work. Claude Code is the default; Codex is an
# alternative backend. Each has a headless (autonomous) and an interactive
# (HITL) invocation — see cmd builders below.
AGENTS = ("claude", "codex")


# --------------------------------------------------------------------------- #
# Small utilities
# --------------------------------------------------------------------------- #

def _resolve(exe: str) -> str:
    """Resolve an executable (handles `.cmd`/`.exe` shims on Windows)."""
    path = shutil.which(exe)
    if not path:
        sys.exit(f"error: `{exe}` not found on PATH. Install it and try again.")
    return path


GH = _resolve("gh")

# Agent executables are resolved lazily and cached, so a codex-only user isn't
# forced to have `claude` on PATH (and vice versa).
_EXE_CACHE: dict[str, str] = {}


def agent_exe(agent: str) -> str:
    if agent not in _EXE_CACHE:
        _EXE_CACHE[agent] = _resolve(agent)  # exe name matches the agent name
    return _EXE_CACHE[agent]


def log(msg: str = "") -> None:
    """Print to stdout and append to the run log."""
    print(msg, flush=True)
    with LOG_FILE.open("a", encoding="utf-8") as fh:
        fh.write(msg + "\n")


def gh_json(args: list[str]):
    """Run a `gh` command that returns JSON and parse it."""
    out = subprocess.run(
        [GH, *args],
        capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    if out.returncode != 0:
        sys.exit(f"error: gh {' '.join(args)} failed:\n{out.stderr.strip()}")
    return json.loads(out.stdout)


# --------------------------------------------------------------------------- #
# Parsing issue bodies
# --------------------------------------------------------------------------- #

_HASH_REF = re.compile(r"#(\d+)")


def _section(body: str, heading: str) -> str:
    """Return the text of a `## <heading>` section, up to the next `## `."""
    lines = (body or "").splitlines()
    out, capturing = [], False
    for line in lines:
        if re.match(r"^\s*#{1,6}\s", line):
            if capturing:
                break
            capturing = re.match(rf"^\s*#{{1,6}}\s+{re.escape(heading)}\s*$",
                                 line, re.IGNORECASE) is not None
            continue
        if capturing:
            out.append(line)
    return "\n".join(out)


def task_numbers_from_spec(spec_body: str) -> list[int]:
    """Child issue numbers from the spec's `## Tasks` checklist, in order."""
    section = _section(spec_body, "Tasks")
    seen, ordered = set(), []
    for m in _HASH_REF.finditer(section):
        n = int(m.group(1))
        if n not in seen:
            seen.add(n)
            ordered.append(n)
    return ordered


def blockers_in_body(body: str) -> set[int]:
    """Issue numbers this task is `## Blocked by`. Empty if it says 'None'."""
    section = _section(body, "Blocked by")
    if re.search(r"\bnone\b", section, re.IGNORECASE) and not _HASH_REF.search(section):
        return set()
    return {int(m.group(1)) for m in _HASH_REF.finditer(section)}


# --------------------------------------------------------------------------- #
# GitHub queries
# --------------------------------------------------------------------------- #

ISSUE_FIELDS = "number,state,title,labels,body,parent,blockedBy"


def fetch_issues() -> dict[int, dict]:
    """Every issue in the repo, indexed by number, with the fields we need."""
    rows = gh_json([
        "issue", "list", "--state", "all", "--limit", "800",
        "--json", ISSUE_FIELDS,
    ])
    return {row["number"]: row for row in rows}


def labels_of(issue: dict) -> set[str]:
    return {lbl["name"] for lbl in issue.get("labels", [])}


def is_open(issue: dict | None) -> bool:
    return bool(issue) and issue.get("state", "").upper() == "OPEN"


def parent_of(issue: dict) -> int | None:
    """The spec this issue is a native sub-issue of, if any."""
    parent = issue.get("parent")
    return parent.get("number") if parent else None


def open_blockers(issue: dict, issues: dict[int, dict]) -> list[int]:
    """Open issues blocking this task — native links plus `## Blocked by` refs.

    A native blocker carries its own state, so it is trusted directly; a parsed
    one is looked up (and an unknown number is treated as not blocking, the same
    as before).
    """
    blocked = {node["number"] for node in issue.get("blockedBy", {}).get("nodes", [])
               if node.get("state", "").upper() == "OPEN"}
    blocked |= {n for n in blockers_in_body(issue.get("body") or "")
                if is_open(issues.get(n))}
    return sorted(blocked)


def spec_children(spec_num: int, issues: dict[int, dict]) -> list[int]:
    """Candidate task numbers for a spec, in dependency order.

    The spec's `## Tasks` checklist comes first when it has one, because it is
    the only source that carries an explicit order. Native sub-issues and issues
    whose body says `Part of #<spec>` are appended in issue-number order — that
    covers specs written without a checklist, and fix issues filed by
    `/review-and-task` mid-run. Ordering the tail by number is enough: real
    dependencies are enforced by `open_blockers`, not by position.
    """
    spec = issues.get(spec_num) or _view_issue(spec_num)
    ordered = task_numbers_from_spec(spec.get("body", ""))

    part_of = re.compile(rf"Part of\s+#{spec_num}\b", re.IGNORECASE)
    extra = [n for n, it in sorted(issues.items())
             if n not in ordered and n != spec_num
             and (parent_of(it) == spec_num or part_of.search(it.get("body") or ""))]
    return ordered + extra


def _view_issue(num: int) -> dict:
    return gh_json(["issue", "view", str(num), "--json", ISSUE_FIELDS])


# --------------------------------------------------------------------------- #
# Task selection
# --------------------------------------------------------------------------- #

def is_spec(issue: dict) -> bool:
    """A nested PRD rather than a task.

    A spec can be a child of a larger spec and still carry `ready-for-agent`,
    meaning its own tasks are ready. Handing the whole spec to one agent session
    is never what that label means — loop over it separately instead.
    """
    return SPEC_LABEL in labels_of(issue)


def available_task(spec_num: int, issues: dict[int, dict], skip: set[int]):
    """First OPEN, ready-for-agent, unblocked, non-skipped, non-spec child."""
    for n in spec_children(spec_num, issues):
        it = issues.get(n)
        if not is_open(it) or n in skip:
            continue
        if READY_LABEL not in labels_of(it) or is_spec(it):
            continue
        if open_blockers(it, issues):
            continue
        return n, it["title"]
    return None


def summarize_remaining(spec_num: int, issues: dict[int, dict], skip: set[int]) -> str:
    """Explain why no task is available."""
    children = spec_children(spec_num, issues)
    open_children = [n for n in children if is_open(issues.get(n))]
    if not open_children:
        return f"All {len(children)} task(s) under spec #{spec_num} are closed — spec complete."

    specs = [n for n in open_children if is_spec(issues[n])]
    tasks = [n for n in open_children if n not in specs]
    human = [n for n in tasks if HUMAN_LABEL in labels_of(issues[n])]
    skipped = [n for n in open_children if n in skip]
    blocked = [n for n in tasks
               if READY_LABEL in labels_of(issues[n])
               and open_blockers(issues[n], issues)]
    parts = []
    if specs:
        parts.append(f"{len(specs)} nested spec(s) to loop over separately ({_fmt(specs)})")
    if human:
        parts.append(f"{len(human)} need a human ({_fmt(human)})")
    if blocked:
        parts.append(f"{len(blocked)} blocked ({_fmt(blocked)})")
    if skipped:
        parts.append(f"{len(skipped)} skipped after repeated failures ({_fmt(skipped)})")
    detail = "; ".join(parts) or "reason unclear — inspect the issues manually"
    return f"No AFK task available under spec #{spec_num}. Remaining open: {detail}."


def _fmt(nums: list[int]) -> str:
    return ", ".join(f"#{n}" for n in nums)


# --------------------------------------------------------------------------- #
# Running one iteration
# --------------------------------------------------------------------------- #

def build_prompt(issue_num: int, issue_title: str, spec_num: int) -> str:
    template = PROMPT_FILE.read_text(encoding="utf-8")
    return (template
            .replace("{{ISSUE_NUMBER}}", str(issue_num))
            .replace("{{ISSUE_TITLE}}", issue_title)
            .replace("{{SPEC_NUMBER}}", str(spec_num)))


HITL_NOTE = ("\n\nThis is an interactive HITL session — pause and ask the human "
             "whenever a decision needs their input.")


def _headless_cmd(agent: str, prompt: str, model: str | None,
                  permission_mode: str) -> tuple[list[str], dict[str, str]]:
    """Argv + extra env for a fully-autonomous one-shot run of `agent`."""
    exe = agent_exe(agent)
    if agent == "codex":
        # `codex exec` is non-interactive; `--yolo` bypasses approvals and the
        # sandbox so it can implement and commit without prompting.
        cmd = [exe, "exec", "--yolo"]
        if model:
            cmd += ["--model", model]
        return cmd + [prompt], {}
    # claude: headless `--print`; disable background tasks so nothing lingers
    # between iterations and the run stays deterministic.
    cmd = [exe, "--permission-mode", permission_mode]
    if model:
        cmd += ["--model", model]
    return cmd + ["--print", prompt], {"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": "1"}


def _interactive_cmd(agent: str, prompt: str, model: str | None,
                     permission_mode: str) -> list[str]:
    """Argv for an interactive (HITL) run of `agent`, attached to the terminal."""
    exe = agent_exe(agent)
    body = prompt + HITL_NOTE
    if agent == "codex":
        # Bare `codex` (no `exec`, no `--yolo`) launches the interactive TUI with
        # approvals on, so the human is asked before commands run.
        cmd = [exe]
        if model:
            cmd += ["--model", model]
        return cmd + [body]
    cmd = [exe, "--permission-mode", permission_mode]
    if model:
        cmd += ["--model", model]
    return cmd + [body]


def run_agent(agent: str, prompt: str, model: str | None, permission_mode: str) -> int:
    """Run one headless session, streaming its output to us and the log."""
    cmd, extra_env = _headless_cmd(agent, prompt, model, permission_mode)
    env = os.environ.copy()
    env.update(extra_env)
    proc = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, encoding="utf-8", errors="replace", env=env,
    )
    with LOG_FILE.open("a", encoding="utf-8") as fh:
        for line in proc.stdout:  # type: ignore[union-attr]
            sys.stdout.write(line)
            sys.stdout.flush()
            fh.write(line)
    return proc.wait()


def run_agent_interactive(agent: str, prompt: str, model: str | None,
                          permission_mode: str) -> int:
    """Run one interactive session attached to the terminal, for HITL use.

    Unlike `run_agent`, this is not headless and does not bypass approvals — it
    hands the driver-selected task to a normal interactive session so a human can
    steer, answer questions, and make the calls the agent shouldn't make alone.
    """
    # No stdout/stderr redirection: the session inherits this terminal.
    return subprocess.run(_interactive_cmd(agent, prompt, model, permission_mode)).returncode


# --------------------------------------------------------------------------- #
# Main loop
# --------------------------------------------------------------------------- #

def parse_spec_arg(raw: str) -> int:
    m = re.search(r"(\d+)\s*$", raw.strip())
    if not m:
        sys.exit(f"error: could not read an issue number from '{raw}'.")
    return int(m.group(1))


def main() -> int:
    ap = argparse.ArgumentParser(description="Ralph loop over a spec issue's child tasks.")
    ap.add_argument("spec", help="Spec (PRD) issue number or URL to loop over.")
    ap.add_argument("--max-iterations", type=int, default=10,
                    help="Stop after this many agent runs (default: 10).")
    ap.add_argument("--max-attempts", type=int, default=2,
                    help="Give up on a single task after this many failed runs "
                         "and skip it (default: 2).")
    ap.add_argument("--agent", choices=AGENTS, default="claude",
                    help="Coding agent backend (default: claude).")
    ap.add_argument("--model", default=None,
                    help="Model to use. Defaults to 'opus' for claude, and to "
                         "codex's own configured default for codex.")
    ap.add_argument("--permission-mode", default="auto",
                    help="claude --permission-mode (default: auto). Ignored for codex.")
    ap.add_argument("--list", action="store_true",
                    help="Print the currently available task and exit.")
    ap.add_argument("--once", action="store_true",
                    help="Human-in-the-loop: pick the next available task and run "
                         "ONE interactive session on it, then exit "
                         "(the successor to the old ralph-once.sh).")
    args = ap.parse_args()

    spec_num = parse_spec_arg(args.spec)

    # Model default is agent-specific: 'opus' for claude, codex's own default
    # (pass nothing) for codex.
    model = args.model or ("opus" if args.agent == "claude" else None)

    if not PROMPT_FILE.exists():
        sys.exit(f"error: prompt file not found at {PROMPT_FILE}")

    stamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%SZ")
    LOG_FILE.write_text(f"# Ralph loop over spec #{spec_num} — started {stamp}\n",
                        encoding="utf-8")

    if args.list:
        issues = fetch_issues()
        pick = available_task(spec_num, issues, skip=set())
        if pick:
            log(f"Next available task: #{pick[0]} — {pick[1]}")
        else:
            log(summarize_remaining(spec_num, issues, skip=set()))
        return 0

    if args.once:
        issues = fetch_issues()
        pick = available_task(spec_num, issues, skip=set())
        if pick is None:
            log(summarize_remaining(spec_num, issues, skip=set()))
            return 0
        issue_num, issue_title = pick
        log(f"HITL session ({args.agent}) on task #{issue_num}: {issue_title}")
        prompt = build_prompt(issue_num, issue_title, spec_num)
        return run_agent_interactive(args.agent, prompt, model, args.permission_mode)

    attempts: dict[int, int] = {}
    skip: set[int] = set()

    for i in range(1, args.max_iterations + 1):
        issues = fetch_issues()
        pick = available_task(spec_num, issues, skip)
        if pick is None:
            log("")
            log(summarize_remaining(spec_num, issues, skip))
            _notify(f"Ralph loop for spec #{spec_num} finished after {i - 1} iteration(s).")
            return 0

        issue_num, issue_title = pick
        log("")
        log("=" * 72)
        log(f"Iteration {i}/{args.max_iterations} ({args.agent}) — task #{issue_num}: {issue_title}")
        log("=" * 72)

        prompt = build_prompt(issue_num, issue_title, spec_num)
        code = run_agent(args.agent, prompt, model, args.permission_mode)
        if code != 0:
            log(f"warning: {args.agent} exited with code {code}.")

        # Authoritative check: did the agent actually close the issue?
        fresh = _view_issue(issue_num)
        if not is_open(fresh):
            log(f"✓ Task #{issue_num} closed. Moving on.")
            attempts.pop(issue_num, None)
            continue

        attempts[issue_num] = attempts.get(issue_num, 0) + 1
        if attempts[issue_num] >= args.max_attempts:
            skip.add(issue_num)
            log(f"✗ Task #{issue_num} still open after {attempts[issue_num]} "
                f"attempt(s) — skipping it for the rest of this run.")
        else:
            log(f"… Task #{issue_num} still open (attempt {attempts[issue_num]} of "
                f"{args.max_attempts}) — will retry, continuing WIP.")

    log("")
    log(f"Hit max iterations ({args.max_iterations}) with tasks still open — stopping.")
    _notify(f"Ralph loop for spec #{spec_num} hit max iterations.")
    return 1


def _notify(message: str) -> None:
    """Best-effort desktop notification via `tt`, mirroring the old loop."""
    tt = shutil.which("tt")
    if tt:
        try:
            subprocess.run([tt, "notify", message], check=False,
                           capture_output=True, text=True)
        except Exception:
            pass


if __name__ == "__main__":
    raise SystemExit(main())
