OUTPUT_DIR ?= bin

.PHONY: all build clean lint lint.fix security test test.integration test-s3-integration fmt changelog hooks.install pre-commit help

## Build -------------------------------------------------------------------

all: build

build: ## Build the binary into bin/
	@echo "Building verda ..."
	@mkdir -p $(OUTPUT_DIR)
	@go build -o $(OUTPUT_DIR)/verda ./cmd/verda/

clean: ## Remove build artifacts
	@rm -rf $(OUTPUT_DIR)

## Quality -----------------------------------------------------------------

lint: ## Run golangci-lint on all packages
	@golangci-lint run ./...

lint.fix: ## Run golangci-lint with auto-fix
	@golangci-lint run --fix ./...

security: ## Run gosec-only scan mirroring CI (ignores .golangci.yaml, so test files are scanned too)
	@golangci-lint run --no-config -E gosec ./...

test: ## Run all tests
	@go test -count=1 ./...

test.integration: build ## Run integration tests (requires staging credentials in [test] profile)
	@cp $(OUTPUT_DIR)/verda /usr/local/bin/verda-test
	@VERDA_BIN=$(CURDIR)/$(OUTPUT_DIR)/verda go test -tags=integration -v -count=1 -timeout=5m ./tests/integration/

test-s3-integration: build ## Run S3 data-plane smoke test against a live endpoint (requires VERDA_S3_* env vars)
	@VERDA_BIN=$(CURDIR)/$(OUTPUT_DIR)/verda VERDA_S3_INTEGRATION=1 \
		go test -tags=integration -count=1 -timeout=5m -v ./tests/integration/ -run TestS3

fmt: ## Format code with gofmt and goimports
	@gofmt -w .
	@goimports -w -local github/verda-cloud/verda-cli .
	@go mod tidy

## Release -----------------------------------------------------------------

changelog: ## Generate CHANGELOG.md (requires VERSION, e.g. make changelog VERSION=v1.0.0)
ifndef VERSION
	$(error VERSION is required. Usage: make changelog VERSION=v1.0.0)
endif
	@git-cliff --tag $(VERSION) -o CHANGELOG.md

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
