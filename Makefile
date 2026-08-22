# smith — common developer tasks. Run from the module root.
# `make check` is the full quality gate CI runs.

BIN := bin/smith
PKG := ./...

# Dev tool versions, pinned. CI installs golangci-lint at GOLANGCI_LINT_VERSION
# (read back through `make tools-version`) so the linter that gates a PR and the
# one a developer runs locally are the same build, not two floating ones.
GOLANGCI_LINT_VERSION := v2.12.2
GOVULNCHECK_VERSION := v1.6.0

.PHONY: check build test race cover fmt lint vet fmt-check vuln snapshot tidy tools tools-install tools-version clean

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

vuln: ## govulncheck (skipped with a note if not installed) — CI runs this as a gate
	@command -v govulncheck >/dev/null 2>&1 && govulncheck $(PKG) || echo "govulncheck not installed; skipping (run 'make tools' for install hint)"

snapshot: ## build all release targets locally, no publish (local twin of CI's build job)
	goreleaser build --snapshot --clean

tidy: ## go mod tidy + verify
	go mod tidy
	go mod verify

tools-install: ## install the dev tools into GOPATH/bin, at the versions CI uses
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install golang.org/x/tools/cmd/goimports@latest
	@echo ""
	@echo "Installed into $$(go env GOPATH)/bin. That must be on PATH:"
	@echo '  export PATH="$$PATH:$$(go env GOPATH)/bin"'
	@echo "Without it 'make lint' and 'make vuln' skip with a note and the gate"
	@echo "still passes, so 'make check' reports green having linted nothing."

tools: ## print what tools-install would install, without installing
	@echo "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"
	@echo "go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)"
	@echo "go install golang.org/x/tools/cmd/goimports@latest"
	@echo ""
	@echo "They install into $$(go env GOPATH)/bin, which must be on PATH:"
	@echo '  export PATH="$$PATH:$$(go env GOPATH)/bin"'
	@echo "Without it 'make lint' and 'make vuln' skip with a note and the gate"
	@echo "still passes, so 'make check' reports green having linted nothing."

tools-version: ## print the pinned golangci-lint version (CI installs this)
	@echo $(GOLANGCI_LINT_VERSION)

clean: ## remove build artifacts
	rm -rf bin
