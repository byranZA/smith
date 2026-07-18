---
name: coding-standards
description: Coding standards for the smith Go CLI. Use when writing, modifying, or reviewing any code in this project.
---

# Coding Standards

## Design Philosophy

**Deep modules** (Ousterhout, _A Philosophy of Software Design_): small interface, deep implementation. A few methods with simple parameters hiding complex logic behind them. When designing, ask: can I reduce the number of methods? Can I simplify the parameters? Can I hide more complexity inside?

**Accept dependencies, don't create them** — pass external dependencies in rather than constructing them internally. **Return results, don't produce side effects** — a function that returns a value is easier to test than one that mutates state.

## Testing

Tests verify **behavior through public interfaces**, not implementation details. Code can change entirely; tests shouldn't break unless behavior changed.

### Good tests

- Exercise real code paths through public APIs
- Describe _what_ the system does, not _how_
- Survive internal refactors
- One logical assertion per test

### Bad tests (red flags)

- Mocking internal collaborators (your own classes/modules)
- Testing private methods or asserting on call counts/order
- Verifying through external means (e.g. querying a DB) instead of the interface
- Test name describes HOW not WHAT

### Mocking

Mock at **system boundaries** only: external APIs, time/randomness, the OS/file system when a real instance isn't practical. **Never mock your own packages.** If something is hard to test without mocking internals, redesign the interface — in Go, accept a narrow interface so a real or fake collaborator can be passed in. Prefer a real temp dir / temp DB over a mock where practical.

## TDD Workflow

All new or modified functions containing calculation, transformation, or business logic **must** have tests driven by red-green-refactor.

Work in **vertical slices** — complete one thin feature end-to-end (test + implementation) before starting the next:

```
RED->GREEN: test1->impl1
RED->GREEN: test2->impl2
RED->GREEN: test3->impl3
```

Each test responds to what you learned from the previous cycle. **Never write all tests first.** Never refactor while RED — get to GREEN first.

### Extract, test, then wire

1. **Extract** logic into a pure, testable module (service, reducer, utility)
2. **Test** it directly — given inputs, assert on outputs
3. **Wire** into the framework layer (view, component) only after tests are green

The framework layer should be thin — call the tested function, return/render results.

## Completion Checklist (BLOCKING)

Do NOT consider work complete until every item passes:

- [ ] **Formatting** — `gofmt -l .` reports nothing on changed files (use `goimports`)
- [ ] **Doc comments** — every exported identifier and every package has a doc comment beginning with its name
- [ ] **Errors handled** — no discarded errors (`_`); failures wrapped with `%w` and context
- [ ] **Tests** — every logic function has corresponding tests and they pass (`go test ./...`); `-race` for concurrent code
- [ ] **Vet** — `go vet ./...` is clean on changed packages
- [ ] **Full suite** — `go build ./...` and `go test ./...` all pass

Fix failures before marking done. This checklist is a hard gate, not a suggestion.

## Language-Specific Standards

- **Go**: See [GO.md](GO.md)
