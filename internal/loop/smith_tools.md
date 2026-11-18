## Smith tools

This run serves smith tools: actions the loop performs for you, called from your shell as `smith tool <name>`.

- `smith tool stack-up`: re-run the stack's recorded up in this worktree. Call it after you change an image, the compose file or the environment, so the running stack picks the change up. Code reloading is the repo's job, so do not call it for a code change.

Run `smith tool list` to see what is served.
