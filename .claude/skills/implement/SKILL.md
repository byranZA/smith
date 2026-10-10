---
name: implement
description: Implement a GitHub issue in its own git worktree, test-first, ending on a local commit. Usage: /implement <issue number>
disable-model-invocation: true
---

# Implement an issue in a worktree

The user names one issue. Work it to a commit in a fresh worktree, leaving the main checkout untouched.

## The worktree is the only checkout

Every shell call starts back in the main checkout, so each command and each file path must name the worktree:

- Shell: `cd ../smith-<n> && <command>`, or `git -C ../smith-<n> …`.
- Read, Edit, Write: absolute paths under the worktree, such as `/Users/…/smith-<n>/internal/cli/machine.go`.

A path into the main checkout is a wrong edit, however right the change.

## Steps

1. **Read the issue.** Run `gh issue view <n> --comments`, and the spec its `parent:` line names, if it has one. Its acceptance criteria and quality gates are the definition of done. Go on only when it carries `ready-for-agent`. For any other triage label (see `docs/agents/triage-labels.md`), or a closed issue, report the label and stop.
2. **Cut the worktree.** Branch from the remote, which leaves out whatever the main checkout has uncommitted:

   ```sh
   git fetch origin
   git worktree add -b <type>/<n>-<slug> ../smith-<n> origin/main
   ```

   `<type>` is the Conventional Commits type the change will use (`feat`, `fix`, `chore`…). The step is done when `git -C ../smith-<n> status` shows a clean tree on the new branch.
3. **Implement test-first.** Follow the `tdd` skill and the `coding-standards` skill. Each behaviour starts as a test you watched go *red* for the reason the issue describes, then goes green.
4. **Verify.** Run the checks `AGENTS.md` names in the worktree. The step is done when `make check` exits zero and its output carries no "not installed; skipping" note. A skip note means a tool never ran: install it with `make tools-install` and run the checks again.
5. **Commit.** One Conventional Commit referencing the issue, as `AGENTS.md` says. Leave it unpushed.

## Report

Give the worktree path, the branch, the commit sha, and each acceptance criterion with the test or check that shows it met. Push, open a PR, or remove the worktree when the user asks.
