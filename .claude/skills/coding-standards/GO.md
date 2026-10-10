# Go Standards

`go.mod` holds the Go version and dependencies. The binary is cgo-free, which is why persistence uses `modernc.org/sqlite`; keep new dependencies pure Go.

## Layout

- **`main` stays tiny.** `cmd/smith/main.go` only calls `cli.Execute()`; everything else lives under `internal/`.
- **One package per domain concept**, named for what it _is_ (`blueprint`, `workspace`), never for a layer (`handlers`, `services`). Use the terms in `CONTEXT.md`.
- **Shallow and earned.** One or two levels under `internal/`; add a package when a concept earns its own boundary.

## Naming

- **No stutter.** The package name is part of every call site: `config.Load`, not `config.LoadConfig`.
- **Initialisms keep one case**: `ID`, `URL`, `HTTP`, so `projectID`.
- **Short locals and receivers**: `i`, `err`, `b` for a `strings.Builder`, one-letter receivers (`func (m Mount) ...`) used consistently across a type's methods.
- **Interfaces are small and named for behaviour**, often `-er` (`Runner`).
- **Errors**: sentinels are `ErrXxx` (`errXxx` unexported); error types are `XxxError`.

## Doc comments

Doc comments are the only comments in this codebase. This is a house rule, stricter than Go's doc comment spec, which allows "why" comments in function bodies. When a line needs explaining, extract a function or variable whose name carries the explanation.

- **Every package** has a package comment on one file: `// Package config reads and writes the smith config.`
- **Every function and method**, exported or not, and every exported type, const and var, has a doc comment of **one or two sentences**. It starts with the identifier's name, ends with a period, and states the contract a caller relies on: what it returns or does, not how. A boolean function "reports whether".

```go
// Load reads the config at path. A missing file yields the zero Config and no error.
func Load(path string) (Config, error) {
```

- **Tests carry their explanation in their names**: `TestLoadMissingFileYieldsZeroConfig`, subtests like `"sibling path is outside parent"`. A test function has no doc comment. Any other declaration in a `_test.go` file (helper, fake, const) gets a **one-line** doc comment, and only when its name cannot say it.
- **Directives are not comments.** `//go:build`, `//go:embed` and `//nolint:` stay, but only for a tool that the build or `make check` runs. A `//nolint` names its linter and says why: `//nolint:errcheck // removal is best effort`.

## Errors

- **Handle every error**: return it, handle it, or (only in `main` or a truly unrecoverable case) exit. Never assign one to `_`, and use the two-value form of a type assertion.
- **Wrap with context** using `%w` so callers can `errors.Is`/`errors.As` and the chain reads top-down:

```go
fi, err := os.Stat(path)
if err != nil {
    return nil, fmt.Errorf("stat config %s: %w", path, err)
}
```

- **Error strings** are lowercase with no trailing punctuation, because they get wrapped: `"malformed config %s: %q"`.
- **Sentinel errors** for conditions callers branch on; **error types** when callers need structured fields.
- **`panic` is for programmer bugs only** (impossible states), never for bad input or missing files.

## Code style

- **Formatting**: `gofmt`, with `goimports` grouping imports as stdlib, then everything else.
- **Accept interfaces, return concrete types.** Define the narrow interface in the package that consumes it, not beside the implementation.
- **Early returns.** Guard clauses at the top; the happy path stays unindented, with no `else` after a `return`.
- **Constructors** are `NewXxx`, returning the concrete type (and an `error` if construction can fail). Make the zero value a valid starting state where practical.
- **Explicit wiring.** Reach for `init()` only when nothing else works.
- **A `switch` over a smith enum** lists every value or has a `default`, so a new value cannot fall through unnoticed.

## Concurrency

Use goroutines only for real concurrent work.

- **One coordination tool per piece of data**: channels to coordinate goroutines, a `sync.Mutex` to guard shared state.
- **The creator owns the goroutine's lifecycle.** Know how every goroutine exits, and propagate cancellation with `ctx context.Context` as the first parameter.

## Testing

Use the standard `testing` package only: no assertion, mocking or diff libraries. Test files sit beside the code, in the same package for white-box tests or `package foo_test` to hold a test to the public API.

Table-driven tests with one `t.Run` per case are the default:

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

- **Every case runs the same assertions.** When cases need different checks (a `wantCall bool`, a per-case check func), split the table.
- **Failures identify the function and its input, then got before want**: `t.Errorf("Load(%q) = %+v, want %+v", path, got, want)`. A reader diagnoses the failure without opening the test. `t.Fatalf` when continuing makes no sense, `t.Errorf` otherwise.
- **Compare whole values.** Build the expected struct and compare with `reflect.DeepEqual`, rather than checking field by field.
- **Check error kinds with `errors.Is`/`errors.As`.** Match `err.Error()` text only when the message itself is the behaviour, as in user-facing CLI output.
- **Use the `testing` helpers**: `t.Helper()` first in every helper, `t.TempDir()`, `t.Setenv()`, `t.Chdir()`, and `t.Context()` for a context.
- **`t.Parallel()`** for independent tests, in the parent and its subtests alike.

## Gate

[docs/development.md](../../../docs/development.md) owns machine setup, the tool pins and the gate. Read it once on a fresh checkout.

**`make lint` and `make vuln` skip with a note and exit zero when their tool is absent**, so `make check` can report green having linted nothing. Read the output for the skip note; `make tools-install` fixes it.

Lint (`.golangci.yml`) checks exported doc comments, error strings and error wrapping. Comments in function bodies, doc comment length and test quality are not checked by any tool: they are on you and on review.
