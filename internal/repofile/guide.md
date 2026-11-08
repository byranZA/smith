# smith

This repo's tasks are worked by smith's loop, which hands a coding agent one task at a time.

- Run `smith help agents` for the guide to driving smith, and `smith <verb> --help` for any one verb.
- Before committing design docs to a spec's work branch, run `smith loop list <n>` for the branch the loop will use.
- `.smith/repo.yaml` holds the loop settings and `.smith/prompt.md` is the ejected loop prompt, when the repo has one. `.smith/review.md` is the ejected review prompt, when it has one.
- An agent handed a task by the loop does not run smith: the loop prompt alone instructs it.
- When `.smith/repo.yaml` declares a `stack:`, smith brings it up before your attempt and takes it down when the run ends only when smith is managing the stack for the run. On a box it always does. Off a box it does so only when the run passed `--stack`; without it, the stack is left off and its services may be unavailable. When smith is managing the stack, it is yours to use. Either way, do not start, stop or reset its services yourself, and do not run a second copy beside it.
- `.smith/repo.yaml` may cap the agent runs of one loop run with `limits: {agent_runs: <n>}`, counting every attempt and the review. Unset there is no cap, and `smith loop run --max-iterations` overrides it for one run (`0` lifts it). A run that reaches the cap ends `cap reached`, so stop and hand back what is left rather than work past it.
