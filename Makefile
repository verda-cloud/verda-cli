OUTPUT_DIR ?= bin

# Single source of truth for the Go version: the go directive in go.mod (CI reads it via go-version-file).
GO_VERSION := $(shell sed -n 's/^go //p' go.mod)
GOLANGCI_LINT_VERSION := v2.5.0
GOLANGCI_LINT := GOTOOLCHAIN=go$(GO_VERSION) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# Toolchain pinned because each Go release adds or removes fixers; the integration tag brings tests/integration into scope.
GOFIX := GOTOOLCHAIN=go$(GO_VERSION) go fix -tags=integration

.PHONY: all build clean run.sandbox lint lint.fix security fix fix.check test test.integration test-s3-integration fmt changelog changelog.unreleased hooks.install pre-commit help

## Build -------------------------------------------------------------------

all: build

build: ## Build the binary into bin/
	@echo "Building verda ..."
	@mkdir -p $(OUTPUT_DIR)
	@go build -o $(OUTPUT_DIR)/verda ./cmd/verda/

clean: ## Remove build artifacts
	@rm -rf $(OUTPUT_DIR)

# Never drive the binary against the real ~/.verda: auth login replaces a profile
# with no warning, and a clobbered client secret cannot be read back from the API.
# VERDA_HOME redirects the whole config dir; VERDA_SHARED_CREDENTIALS_FILE does not.
run.sandbox: build ## Run the binary against a throwaway config dir, e.g. make run.sandbox ARGS="auth login"
	@dir=$$(mktemp -d) && echo "VERDA_HOME=$$dir" && VERDA_HOME=$$dir $(OUTPUT_DIR)/verda $(ARGS)

## Quality -----------------------------------------------------------------

lint: ## Run golangci-lint on all packages
	@$(GOLANGCI_LINT) run ./...

lint.fix: ## Run golangci-lint with auto-fix
	@$(GOLANGCI_LINT) run --fix ./...

# --default=none keeps golangci-lint's "standard" set out, so only gosec findings are reported.
security: ## Run gosec-only scan, as CI does (ignores .golangci.yaml, so test files are scanned too)
	@$(GOLANGCI_LINT) run --no-config --default=none -E gosec ./...

# Two passes: applying one modernization can enable another (https://go.dev/blog/gofix).
fix: ## Apply go fix modernizers
	@$(GOFIX) ./...
	@$(GOFIX) ./...

fix.check: ## Fail if go fix has pending modernizations (CI)
	@$(GOFIX) -diff ./... || { echo "Pending go fix changes. Run 'make fix'"; exit 1; }

test: ## Run all tests (with race detector)
	@go test -race -count=1 ./...

test.integration: build ## Run integration tests (requires staging credentials in [test] profile)
	@cp $(OUTPUT_DIR)/verda /usr/local/bin/verda-test
	@VERDA_BIN=$(CURDIR)/$(OUTPUT_DIR)/verda go test -tags=integration -v -count=1 -timeout=5m ./tests/integration/

test-s3-integration: build ## Run S3 data-plane smoke test against a live endpoint (requires VERDA_S3_* env vars)
	@VERDA_BIN=$(CURDIR)/$(OUTPUT_DIR)/verda VERDA_S3_INTEGRATION=1 \
		go test -tags=integration -count=1 -timeout=5m -v ./tests/integration/ -run TestS3

fmt: ## Format code with gofmt and goimports
	@gofmt -w .
	@# local prefix has a known typo (missing .com, same as .golangci.yaml); fixing it would regroup ~100 files — deliberate deferral, see .golangci.yaml.
	@goimports -w -local github/verda-cloud/verda-cli .
	@go mod tidy

## Release -----------------------------------------------------------------

changelog: ## Generate CHANGELOG.md (requires VERSION, e.g. make changelog VERSION=v1.0.0)
ifndef VERSION
	$(error VERSION is required. Usage: make changelog VERSION=v1.0.0)
endif
	@git-cliff --tag $(VERSION) -o CHANGELOG.md

changelog.unreleased: ## Regenerate CHANGELOG.md with pending commits under [Unreleased]
	@git-cliff -o CHANGELOG.md

## Git Hooks ---------------------------------------------------------------

hooks.install: ## Configure git to use githooks/ as the hooks directory
	@git config core.hooksPath githooks
	@echo "Git hooks installed (githooks/)"

pre-commit: ## Run all pre-commit checks manually
	@pre-commit run --all-files

## Help --------------------------------------------------------------------

help: ## Show this help
	@grep -E '^[a-zA-Z_.]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
