# smith

This repo's tasks are worked by smith's loop, which hands a coding agent one task at a time.

- Run `smith help agents` for the guide to driving smith, and `smith <verb> --help` for any one verb.
- Before committing design docs to a spec's work branch, run `smith loop list <n>` for the branch the loop will use.
- `.smith/repo.yaml` holds the loop settings and `.smith/prompt.md` is the ejected loop prompt, when the repo has one.
- An agent handed a task by the loop does not run smith: the loop prompt alone instructs it.
