---
name: coding-standards
description: Coding standards for the smith Go CLI. Use when writing, modifying, or reviewing any code, Markdown, or agent prompt in this project.
---

# Coding Standards

The rules that hold for all work here. Read the companion for what you are touching:

- **Go code and tests**: [GO.md](GO.md)
- **Markdown, docs and agent prompts**: [MARKDOWN.md](MARKDOWN.md)

## Design

**Deep modules** (Ousterhout, _A Philosophy of Software Design_): small interface, deep implementation. A few functions with simple parameters hiding complex logic behind them. When designing, ask: can I reduce the number of functions? Can I simplify the parameters? Can I hide more complexity inside?

**Accept dependencies, don't construct them.** Pass collaborators (a `*sql.DB`, an `io.Writer`, a narrow interface) into constructors and functions. This is what makes code testable without mocking.

**Return results, don't produce side effects.** A function that returns a value is easier to test than one that writes a file. Where the side effect is the point, split it: a pure computation (`Render`) plus a thin writer (`WriteFile`).

## Testing

Tests verify **behaviour through the public interface**. Code can change entirely; a test breaks only when behaviour changed.

- **Every test can go red.** The expected value is a literal worked out by hand from the spec, so a wrong implementation fails the test.
- **One behaviour per test.** A test makes as many checks as it takes to observe the behaviour its name states. When the name needs "and" to join two outcomes that could break on their own, split it into two tests.
- **Name the behaviour.** The test name says _what_ the system does (`TestLoadMissingFileYieldsZeroConfig`), not how.
- **Mock only at system boundaries**: the OS, the network, time, randomness, external processes (agent launches, git). Prefer a real temp dir or temp DB. Your own packages are never mocked; when one seems to need it, redesign its interface so a real or fake collaborator can be passed in.

A **tautological** test passes by construction and can never go red. Review every test for these shapes:

- recomputing the expected value with the code under test's own logic
- asserting a fake returns what it was told to
- asserting a constant equals its literal
- asserting a constructor stored its arguments

Also reject tests that assert on private functions, or that verify through a side channel (querying the DB) instead of the interface.

**Calls are behaviour only at a boundary.** What a boundary fake received, how often, and in what order is the system's visible effect, so assert on it: the loop pushes the branch before handing out the next task, and launches one agent per task. Between your own packages, assert on what comes back, so a test survives the code being restructured.

## TDD

Every new or modified function containing calculation, transformation, or business logic is driven by red-green-refactor, one **vertical slice** at a time:

```text
RED->GREEN: test1->impl1
RED->GREEN: test2->impl2
RED->GREEN: test3->impl3
```

Each test responds to what the previous cycle taught you, so write one test at a time and watch it go red before writing the code. Refactor only on green.

**Extract, test, then wire.** Put the logic in a domain package under `internal/`, test it there with inputs and outputs, and only then call it from the cobra command in `internal/cli`. A `RunE` parses flags, calls the package, and formats the result.

## Completion checklist (blocking)

Work is complete when every item holds:

- [ ] **Gate**: `make check` passes with lint actually running (see [GO.md](GO.md#gate)), plus `make race` for concurrent code
- [ ] **Comments**: doc comments only, shaped as [GO.md](GO.md#doc-comments) says
- [ ] **Errors**: every error handled or returned, wrapped with `%w` and context
- [ ] **Tests**: every logic function has tests that went red first
- [ ] **Prompts**: every agent or LLM prompt lives in its own `.md` file (see [MARKDOWN.md](MARKDOWN.md#prompts))
