# Verda CLI

Go CLI for Verda Cloud. Cobra commands + Bubble Tea TUI + lipgloss styling.

## Build & Validate

```bash
make build        # Build binary to ./bin/verda
make test         # Run all tests (go test + golangci-lint)
make lint         # Lint only
make pre-commit   # Full pre-commit suite
```

Never use raw `go test ./...` — always `make test` which includes linting.

## Architecture

```
cmd/verda/                    # Entrypoint
internal/verda-cli/
  cmd/cmd.go                  # Root command, command groups
  cmd/util/                   # Factory, IOStreams, helpers, pricing, hostname
  cmd/<domain>/               # One dir per domain (vm, volume, auth, skills, ...)
  options/                    # Global CLI options, credentials
internal/skills/              # Embedded AI skill files (go:embed)
```

### Core Patterns

- **Factory** (`cmd/util/factory.go`): DI for Prompter, Status, VerdaClient, Debug, AgentMode, OutputFormat
- **Wizard engine** (`verdagostack/pkg/tui/wizard`): Multi-step interactive flows
- **Lazy client** (`clientFunc`): API client resolved on first use, not at init
- **API cache** (`apiCache`): Shared across wizard steps to avoid redundant calls

### Local Dependencies

- `verdagostack` replaced locally: `replace github.com/verda-cloud/verdagostack => ../verdagostack`
- Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2 (`charm.land/lipgloss/v2`)
- Never use v1 imports — they won't compile

## Conventions

### Every API-calling command MUST:

1. **Timeout context**: `ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)`
2. **Spinner**: Show spinner during API calls, stop before handling result
3. **Debug output**: `cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "label:", data)`
4. **Dual mode**: Work with flags (non-interactive) AND prompts (interactive) — no partial wizard
5. **Output separation**: Data → `ioStreams.Out`, prompts/warnings/debug → `ioStreams.ErrOut`

### Destructive actions MUST:

- Show warning styling (red bold) before confirmation
- Require `prompter.Confirm()` — return nil on cancel or Esc
- In agent mode (`f.AgentMode()`): require `--yes` flag, never auto-confirm

### Pricing — get this wrong and users get billed wrong:

- Instance `price_per_hour` from API is **per-unit** (per-GPU or per-vCPU)
- Total = `price_per_hour * units` — use `cmdutil.InstanceTotalHourlyCost(inst)`
- Volume: `price_per_month_per_gb` — hourly = `ceil(monthly * size / 730 * 10000) / 10000`
- Never display raw API price as "total" without multiplying

### Credentials

- AWS-style INI at `~/.verda/credentials` with `verda_` prefixed keys
- Profile support via `[profile_name]` sections
- `f.VerdaClient()` handles resolution — returns clear error if not authenticated

## Before Editing Any Command

1. Read the **nearest** `CLAUDE.md` in the command directory (e.g. `cmd/vm/CLAUDE.md`)
2. Read the **nearest** `README.md` for usage examples and flag details
3. Read `.ai/skills/new-command.md` for the full checklist when adding/modifying commands
4. If touching pricing, auth, or agent-mode: plan first, don't code immediately

Per-command docs are auto-maintained by a pre-commit hook.
Manual update: `claude -p "/update-command-knowledge --all" --model sonnet --dangerously-skip-permissions`

## Thinking Depth

| Change Type | Approach |
|-------------|----------|
| Rename, typo, flag default | Just do it |
| New list/describe command | Follow `.ai/skills/new-command.md` checklist |
| New create/wizard flow | Plan first — wizard steps, cache strategy, step dependencies |
| Refactor shared util | Check all callers, run full test suite |
| Pricing logic | Deep think — verify formula against API docs, test with real numbers |
| Auth flow changes | Deep think — test all profiles, expired tokens, missing creds |
| Agent-mode (`--agent`) changes | Deep think — JSON output contract, structured errors, no prompts |

## Validation

Before considering any change complete:

```bash
make build                    # Must compile
make test                     # Must pass (tests + lint)
```

If you modified a command, also verify:
- `./bin/verda <command> --help` renders correctly
- Interactive mode works (prompts appear)
- Non-interactive mode works (flags only, no prompts)
- `--agent -o json` mode works (structured output, no TUI)
- `--debug` shows request/response payloads
