# smith — common developer tasks. Run from the module root.
# `make check` is the full quality gate CI runs.

BIN := bin/smith
PKG := ./...

.PHONY: check build test race cover fmt lint vet fmt-check tidy tools clean

check: fmt-check vet lint test ## full gate: fmt-check + vet + lint + test

build: ## compile the smith binary into ./bin
	go build -o $(BIN) ./cmd/smith

test: ## full test suite
	go test $(PKG)

race: ## tests with the race detector
	go test -race $(PKG)

cover: ## tests with a coverage summary
	go test -cover $(PKG)

fmt: ## format (gofmt + goimports if available)
	gofmt -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

fmt-check: ## fail if any file needs formatting
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

vet: ## report suspicious constructs
	go vet $(PKG)

lint: ## golangci-lint run (skipped with a note if not installed)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping (run 'make tools' for install hint)"

tidy: ## go mod tidy + verify
	go mod tidy
	go mod verify

tools: ## print how to install dev tools
	@echo "install golangci-lint v2: https://golangci-lint.run/welcome/install/"
	@echo "install goimports: go install golang.org/x/tools/cmd/goimports@latest"

clean: ## remove build artifacts
	rm -rf bin
