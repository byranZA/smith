# Markdown and Prompt Standards

## Line format

Write each paragraph or list item as one unwrapped line; editors and GitHub soft-wrap it for display. A hard wrap at a fixed column turns a one-word edit into a reflowed paragraph, so the diff hides what changed.

Start each heading, list, table, and fenced block on its own line with a blank line before it. Give every fenced block a language tag (`go`, `sh`, `yaml`, or `text` for plain output).

## Prompts

Every prompt sent to an agent or LLM lives in its own `.md` file, never as a string literal in code. A prompt in a file is easy to find, reads as prose, and its history shows in `git log` on that file alone.

- Put the prompt file beside the code that sends it and name it for its job (`ralph/ralph-prompt.md`).
- Mark runtime values with `{{NAME}}` placeholders that the code fills in; the code only reads the file and fills in placeholders.
- In Go, load the file with `//go:embed` so the binary stays self-contained, as `internal/onbox` does with `install.sh`.
- A fragment appended only in some cases (a mode note, a retry hint) is a prompt too, and gets its own file.
