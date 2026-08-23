# Go Standards

## Stack

Go 1.26, module `github.com/byranZA/smith`. CLI built on cobra (`github.com/spf13/cobra`). Blueprints and preferences are YAML, parsed via `go.yaml.in/yaml/v3`. Persistence via `modernc.org/sqlite` (pure-Go, cgo-free). IDs via `github.com/google/uuid`. Tests use the standard `testing` package only — no third-party assertion or mocking frameworks.

## Project Structure

Idiomatic Go layout — shallow hierarchies, packages by domain (not by technical layer), `internal/` for everything not meant for external import. Illustrative shape:

```
smith/
├── cmd/smith/          # main package — minimal, wires Execute() and exits
├── internal/           # all application code (compiler-enforced private)
│   ├── cli/            # cobra command surface (thin — parse flags, call packages)
│   ├── config/         # config read/write
│   └── ...             # one package per domain concept
├── go.mod
└── Makefile
```

- **`main` stays tiny.** `cmd/smith/main.go` only calls `cli.Execute()`. All logic lives in `internal/` packages it imports.
- **`internal/` over `pkg/`.** This is an application, not a library; nothing is meant for external import, so the compiler-enforced `internal/` boundary is correct.
- **Package per domain concept.** Each `internal/` package owns one concept and is named for it. Group by what it *is*, not by layer (no `handlers/`, `services/`, `repositories/`).
- **Shallow nesting.** One or two levels under `internal/`. Deep trees increase cognitive load and ugly imports.
- **No excessive nesting; no premature splitting.** Add a package when a concept earns its own boundary, not speculatively.

## Naming

Go favors brevity (the conventions here follow Effective Go and the Google Go style guide).

- **Packages**: short, lowercase, single-word, no underscores or plurals (`gitmount`, not `git_mounts`). The package name is part of every call site (`config.Default`), so don't stutter — `config.Config` is fine but avoid `config.ConfigDefault`.
- **Exported identifiers**: `PascalCase`. Unexported: `camelCase`.
- **Acronyms** keep a consistent case: `ID`, `URL`, `HTTP` — `projectID`, not `projectId`.
- **Short locals**: `i` for index, `err` for errors, `b` for a `strings.Builder`, single-letter receivers (`func (m Mount) ...`). Don't invent `errorMessage` or `indexValue`.
- **Interfaces**: name for behavior, often `-er` (`Reader`, `Runner`). Keep them small — accept interfaces, return concrete types.
- **Match the domain language.** Domain types use the project's canonical terms so the code reads in its ubiquitous language.

## Doc Comments

Go has no docstrings — it has doc comments, and `go doc`/pkg.go.dev render them. They are mandatory on **exported** identifiers and on every package.

- **Every package** has a package comment on one file: `// Package config reads and writes ...`. For a package that warrants more, lead with a paragraph explaining the *why*.
- **Every exported identifier** (func, type, const, var) has a comment that **begins with the identifier's name**:

```go
// Load reads the config at path and returns it. A missing file yields the
// zero Config and no error; a malformed file is an error.
func Load(path string) (Config, error) {
```

- Explain *what and why*, not the type signature (the signature is already visible). Unexported helpers get a comment only when the intent isn't obvious from the code.
- Full sentences, ending with a period.

## Errors

Errors are values — handle them, don't suppress them.

- **Never discard an error** with `_`. Check every returned `error`: handle it, return it, or (only in `main`/truly-unrecoverable cases) `log.Fatal`/`panic`.
- **Wrap with context** using `fmt.Errorf("...: %w", err)` so callers can `errors.Is`/`errors.As` and the chain reads top-down:

```go
fi, err := os.Stat(path)
if err != nil {
    return nil, fmt.Errorf("stat config %s: %w", path, err)
}
```

- **Error strings** are lowercase, no trailing punctuation (they get wrapped): `"malformed config %s: %q"`, not `"Malformed config."`.
- **Sentinel errors** (`var ErrNotFound = errors.New(...)`) for conditions callers branch on; check with `errors.Is`. Custom error types when callers need structured fields; check with `errors.As`.
- **`panic` is for programmer bugs only** (impossible states), never for ordinary failures like bad input or missing files.
- **Return early.** Handle the error and `return` rather than nesting the happy path inside an `else`.

## Code Style

- **`gofmt` is non-negotiable.** All code is `gofmt`-formatted; use `goimports` to also manage import grouping (stdlib, then third-party/local with a blank line between). Never hand-format.
- **Accept interfaces, return structs.** Take the narrowest interface you need as a parameter; return concrete types so callers aren't boxed in.
- **Zero values should be useful.** Design structs so the zero value is a valid starting state where practical.
- **Composition over inheritance** — embed, don't reach for type hierarchies.
- **Keep functions small and flat.** Early returns over nested `if`/`else`; guard clauses at the top.
- **Constructors**: `New...` returning the concrete type (and `error` if construction can fail).
- **No `init()` magic** unless genuinely required; prefer explicit wiring.

## Concurrency

Only reach for goroutines when there is real concurrent work — don't add them speculatively.

- **"Share memory by communicating."** Use channels to coordinate goroutines; use a `sync.Mutex` to protect shared state. Pick one per situation, don't mix them on the same data.
- **The creator owns the goroutine's lifecycle** — know how every goroutine exits. Propagate cancellation with `context.Context`, passed as the first parameter (`ctx context.Context`).
- **Run tests with `-race`** for any package that uses goroutines.

## Dependencies (design)

Mirror the project-wide principles in Go terms:

- **Accept dependencies, don't construct them.** Pass collaborators (a `*sql.DB`, an `io.Writer`) into constructors/functions rather than newing them up inside. This is what makes a function testable without mocking.
- **Return results, don't mutate.** A function that returns a value is easier to test than one that writes a file as a side effect. Where a side effect is the point, split it: a pure computation (`Render`) plus a thin writer (`WriteFile`).

## Testing

Standard `testing` package, no frameworks. Test files are `_test.go` beside the code, in the **same package** for white-box tests (or `package foo_test` to enforce testing through the public API only).

### Table-driven tests + subtests

The idiomatic default: cases as data, one `t.Run` per case for isolation and named output.

```go
func TestWithin(t *testing.T) {
    tests := []struct {
        name          string
        child, parent string
        want          bool
    }{
        {"equal paths", "/a/b", "/a/b", true},
        {"child inside parent", "/a/b/c", "/a/b", true},
        {"sibling", "/a/x", "/a/b", false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := within(tt.child, tt.parent); got != tt.want {
                t.Errorf("within(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
            }
        })
    }
}
```

### Conventions

- **`t.Helper()`** in every test helper so failures point at the caller, not the helper.
- **`t.TempDir()`** for filesystem tests — auto-cleaned, no manual teardown.
- **Failure messages state got vs want**: `t.Errorf("Load() = %v, want %v", got, want)`. Use `t.Fatalf` when continuing makes no sense, `t.Errorf` to report and keep going.
- **`t.Parallel()`** where tests are independent; capture the loop variable first.
- **Test behavior through the public API.** Don't assert on call order or reach past the interface. Mock only at real boundaries (the OS, the network, time) — and prefer a real temp DB / temp dir over a mock when practical.
- **`-race`** for concurrent code; table-driven benchmarks via `b.Run` when measuring.

### Extract, test, then wire (Go shape)

The project-wide "extract → test → wire" maps directly: pure logic in a domain package, tested in isolation, then the `cli` command is the thin wiring layer that calls it. Keep cobra `RunE` functions thin — parse flags, call the package, format the result.

## Commands

Run from the module root. Machine setup, the tool pins, and the gate are owned
by [CONTRIBUTING.md](../../../CONTRIBUTING.md) — read it once on a fresh
checkout; the list is not repeated here so the two cannot drift.

The short version: `make tools-install` then `make check` before you hand work
back, `make race` for anything concurrent. **`make lint` and `make vuln` skip
with a note when their tool is absent and still exit zero**, so `make check` on
a machine without them reports green having linted nothing — CONTRIBUTING.md
explains the failure that follows.

Targeted `go` invocations, for narrowing a run while you work:

```bash
go test ./internal/config/     # single package
go test -run TestWithin ./...  # single test by name
gofmt -l .                     # list files needing formatting (must be empty)
go vet ./...                   # report suspicious constructs
```

Linting uses `golangci-lint` v2, configured in `.golangci.yml`: `revive`
enforces doc comments and naming, `wrapcheck`/`errorlint` enforce the
error-handling rules above.
