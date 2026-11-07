# smith

This repo's tasks are worked by smith's loop, which hands a coding agent one task at a time.

- Run `smith help agents` for the guide to driving smith, and `smith <verb> --help` for any one verb.
- Before committing design docs to a spec's work branch, run `smith loop list <n>` for the branch the loop will use.
- `.smith/repo.yaml` holds the loop settings and `.smith/prompt.md` is the ejected loop prompt, when the repo has one. `.smith/review.md` is the ejected review prompt, when it has one.
- An agent handed a task by the loop does not run smith: the loop prompt alone instructs it.
- When `.smith/repo.yaml` declares a `stack:`, smith has brought that stack up before your attempt and takes it down when the run ends. It is yours to use: do not start, stop or reset its services yourself, and do not run a second copy beside it.
