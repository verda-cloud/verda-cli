# verda

verda command-line application

## Prerequisites

- **Go 1.25+**
- **golangci-lint** (optional, for linting)

## Quick Start

### Build

```bash
make build
```

### Run

```bash
./bin/verda --help
```

## Development

```bash
make test        # Run all tests
make lint        # Run linter
make lint.fix    # Auto-fix lint issues
make clean       # Remove build artifacts
```

### Git hooks (optional)

Sample pre-commit script lives in **`githooks/pre-commit`** (runs `go build`, `go vet`, optional `golangci-lint`, and short tests for affected packages). Wire it once per clone:

```bash
make hooks.install   # sets git config core.hooksPath to githooks/
```

> **Note:** The template repo’s **`hooks/`** directory is only for `verdactl` post-generation scripts and is not part of the scaffolded project. Use **`githooks/`** for git hooks in generated apps.

## Project Structure

```
githooks/                  Sample pre-commit (optional: make hooks.install)
cmd/verda/              Entry point
internal/verda-cli/
  cmd/                     Cobra commands
  cmd/util/                CLI utilities (factory, iostreams)
  options/                 Shared CLI options
```
