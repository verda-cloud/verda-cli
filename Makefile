OUTPUT_DIR ?= bin

.PHONY: all build clean lint lint.fix test hooks.install help

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

test: ## Run all tests
	@go test ./...

## Git Hooks ---------------------------------------------------------------

hooks.install: ## Configure git to use githooks/ as the hooks directory
	@git config core.hooksPath githooks
	@echo "Git hooks installed (githooks/)"

## Help --------------------------------------------------------------------

help: ## Show this help
	@grep -E '^[a-zA-Z_.]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
