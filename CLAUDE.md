# Verda CLI

Command-line interface for Verda Cloud, built with Go + Cobra + Bubble Tea TUI.

## Build & Test

```bash
go build -o ./bin/verda ./cmd/verda/   # Build binary
go test ./...                           # Run all tests
go mod tidy                             # Sync dependencies
```

## Architecture

- `cmd/verda/` -- Entrypoint
- `internal/verda-cli/cmd/` -- Command implementations organized by domain (vm, volume, auth, sshkey, startupscript)
- `internal/verda-cli/cmd/util/` -- Shared: Factory, IOStreams, helpers, templates, hostname utils
- `internal/verda-cli/options/` -- Global CLI options, credentials resolution

### Key Patterns

- **Factory pattern** (`cmd/util/factory.go`): Dependency injection for Prompter, Status, VerdaClient, Debug
- **Wizard engine** (`verdagostack/pkg/tui/wizard`): Multi-step interactive flows (vm create, auth login)
- **Lazy client** (`clientFunc` type): API client resolved on first use, not at wizard start
- **API cache** (`apiCache`): Shared across wizard steps to avoid redundant API calls

### Dependencies (local dev)

- `verdagostack` is replaced locally via `go.mod`: `replace github.com/verda-cloud/verdagostack => ../verdagostack`
- Uses Bubble Tea v2 (`charm.land/bubbletea/v2`) and lipgloss v2 (`charm.land/lipgloss/v2`)

## Conventions

- Every API-calling command must support `--debug` (global flag) -- see `.ai/skills/new-command.md`
- Every API call must use a timeout context and show a spinner
- Commands support both interactive (prompts) and non-interactive (flags) usage
- Output to `ioStreams.Out`, prompts/warnings/debug to `ioStreams.ErrOut`
- Destructive actions require confirmation with warning styling
- Follow the checklist in `.ai/skills/new-command.md` when adding new commands

## Pricing Model

- Instance `price_per_hour` from API is **per-unit** (per-GPU or per-vCPU). Total = `price_per_hour * units`.
- Use `cmdutil.InstanceTotalHourlyCost(inst)` or `cmdutil.InstanceBillableUnits(inst)` from `cmd/util/pricing.go`.
- Volume pricing is `price_per_month_per_gb`. Hourly = `ceil(monthly_per_gb * size / 730 * 10000) / 10000`

## Per-Command Knowledge

Each subcommand directory has its own docs:
- `README.md` — Human-readable: usage, examples, flags, architecture
- `CLAUDE.md` — AI context: gotchas, domain logic, relationships

These are auto-maintained by a pre-commit hook. See `.ai/skills/update-command-knowledge.md`.

When modifying a command, the hook will auto-update its docs on commit.
For manual update: `claude -p "/update-command-knowledge --all" --model sonnet --dangerously-skip-permissions`

## Credentials

- AWS-style INI file at `~/.verda/credentials` with `verda_` prefixed keys
- Keys: `verda_base_url`, `verda_client_id`, `verda_client_secret`, `verda_token`
- Profile support via `[profile_name]` sections
