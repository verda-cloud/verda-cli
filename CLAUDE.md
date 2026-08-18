# Verda CLI

Go CLI for Verda Cloud. Cobra commands + Bubble Tea TUI + lipgloss styling.

## Build & Validate

```bash
make build        # Build binary to ./bin/verda
make test         # Run all tests (go test -race)
make lint         # Lint only (golangci-lint); also run by pre-commit hooks
make pre-commit   # Full pre-commit suite
```

Never use raw `go test ./...` — always `make test` (go test -race). Lint is separate: `make lint`; the pre-commit hooks run both.

## Architecture

```
cmd/verda/                    # Entrypoint
internal/verda-cli/
  cmd/cmd.go                  # Root command, command groups
  cmd/util/                   # Factory, IOStreams, helpers, pricing, hostname
  cmd/<domain>/               # One dir per domain (see per-command docs below)
    CLAUDE.md                 # Domain knowledge, gotchas, edge cases
    README.md                 # Usage examples, flags, architecture notes
  options/                    # Global CLI options, credentials
internal/skills/              # Embedded AI skill files (go:embed)
pkg/                          # In-tree TUI core, log, version (formerly verdagostack)
```

### Per-Command Documentation

Each command directory has its own `CLAUDE.md` (domain knowledge) and `README.md` (usage/architecture). These are the source of truth for command-specific behavior.

| Directory | Docs | Description |
|-----------|------|-------------|
| `cmd/vm/` | CLAUDE.md, README.md | VM create/list/describe/action, wizard, templates |
| `cmd/template/` | CLAUDE.md, README.md | Template create/edit/list/show/delete |
| `cmd/auth/` | CLAUDE.md, README.md | Login, logout, show credentials |
| `cmd/volume/` | CLAUDE.md, README.md | Volume lifecycle, trash, actions |
| `cmd/sshkey/` | CLAUDE.md, README.md | SSH key management |
| `cmd/startupscript/` | CLAUDE.md, README.md | Startup script management |
| `cmd/registry/` | CLAUDE.md, README.md | Container registry (vccr.io): configure, configure-docker (alias login), show, ls, tags, push, copy, delete — beta (enabled by default, marked `(beta)` in `verda --help`) |
| `cmd/update/` | CLAUDE.md, README.md | CLI self-update |
| `cmd/settings/` | CLAUDE.md, README.md | CLI settings management |
| `cmd/objectstorage/` | CLAUDE.md, README.md | S3-style object storage: configure, mb/rb, cp/mv/sync/ls/rm, uploads, presign |
| `cmd/serverless/` | CLAUDE.md, README.md | Serverless containers and batch jobs |
| `cmd/doctor/` | — | Environment diagnostics |
| `cmd/availability/` | — | Instance availability by location |
| `cmd/cost/` | — | Balance, running costs, estimates |
| `cmd/images/` | — | OS image listing |
| `cmd/instancetypes/` | — | Instance type catalog |
| `cmd/locations/` | — | Datacenter locations |
| `cmd/status/` | — | Status dashboard |
| `cmd/ssh/` | — | SSH into instances |
| `cmd/mcp/` | CLAUDE.md, README.md | MCP server (AI-agent tool surface; confirm gates, accepted/completed semantics) |
| `cmd/skills/` | — | AI skills management |
| `cmd/completion/` | — | Shell completions |

### Core Patterns

- **Factory** (`cmd/util/factory.go`): DI for Prompter, Status, VerdaClient, Debug, AgentMode, OutputFormat
- **Wizard engine** (`pkg/tui/wizard`): Multi-step interactive flows
- **Lazy client** (`clientFunc`): API client resolved on first use, not at init
- **API cache** (`apiCache`): Shared across wizard steps to avoid redundant calls

### TUI / Log / Version packages (`pkg/`)

- `pkg/tui` (+ `bubbletea`, `wizard`, `testing`), `pkg/log`, `pkg/version` live in-tree —
  edit them directly like any other code in this repo (they were copied from
  `verdagostack` v1.4.2, which this repo no longer depends on)
- Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2 (`charm.land/lipgloss/v2`)
- Never use v1 imports — they won't compile

## Conventions

### Go House Style — avoid avoidable lint hits

The repo lints with `golangci-lint` via `make lint` (also enforced by the pre-commit hooks, not by `make test`). These are the patterns the linters enforce — write them correctly the first time instead of fixing them in a second pass:

- **HTTP bodies** — use `http.NoBody` for GET/DELETE/etc., never `nil`. Close with `defer func() { _ = resp.Body.Close() }()`, not bare `defer resp.Body.Close()` (errcheck).
- **American English** — `behavior`, `canceled`, `artifact`, `checkered`, `gray`. `misspell` runs with `locale: US` and rejects British spellings in code and comments.
- **Reuse constants** — before writing a string literal that might repeat, grep for an existing one. Current package-level constants worth knowing: `defaultTag` (`"latest"`) in `cmd/registry/refname.go`, `progressJSON` (`"json"`) in `cmd/registry/push.go`, `untaggedLabel` (`"<untagged>"`) in `cmd/registry/format.go`. `goconst` fails on ≥3 occurrences.
- **Strings over fmt.Sprintf** — `"prefix " + s` beats `fmt.Sprintf("prefix %s", s)` when there's only one substitution (perfsprint).
- **Range indexing for structs ≥96 B** — `for i := range xs { x := &xs[i] }` avoids the per-iteration copy `for _, x := range xs` incurs (gocritic rangeValCopy). `ArtifactInfo`, `VMDescribeResult`, etc. all cross the threshold.
- **Intentional `return nil` after error** — prompter cancellation returns an error that we deliberately swallow. Annotate with `return nil //nolint:nilerr // intentional: prompter cancel is a clean exit` so `nilerr` doesn't flag it and the reason survives.
- **No blank line after `{`** — `whitespace` linter flags it. Go straight into the first statement.
- **Type inference over explicit declaration** — `rt := http.DefaultTransport` over `var rt http.RoundTripper = http.DefaultTransport` (staticcheck ST1023).
- **Complexity budgets** — `gocyclo` trips at 20, `nestif` at 5. Extract helpers before you hit them; refactoring after the fact is more churn.

`.golangci.yaml` is the authoritative list — all of the above come from linters enabled there.

### Comment style — write like a senior

- **Default to no comment.** Well-named identifiers carry the meaning. Add a comment only when the *why* is non-obvious: an invariant, a workaround, a gotcha a future reader would miss, an evolution point.
- **One line, identifier-first.** `// resolveContainerName: args[0], else picker; agent requires <name>.` beats a three-line paragraph.
- **Never narrate WHAT.** `// Loop over deployments and build labels` is noise — delete it.
- **Capture invariants, not history.** `// Describe still succeeds if status RPC fails.` is durable. `// Added for ticket VC-1234` rots — put it in the commit message.
- **Flag known evolution points.** `// if SDK gains json:"status", switch to explicit fields.` documents a future-failure mode so the next reader doesn't have to rediscover it.
- **Delete when the reason expires.** Workaround landed, gotcha fixed, SDK gap closed → remove the comment in the same commit.

### Every API-calling command MUST:

1. **Timeout context**: `ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)` for control-plane calls. Data-plane transfers (registry push/copy, object-storage cp/mv/sync) run on `cmd.Context()` — Ctrl+C is the stop signal; a multi-GB transfer legitimately outlives `--timeout`. Interactive prompts also get `cmd.Context()`, and work resumed after a prompt re-bounds its API ctx so prompt think-time can't drain the budget. The shared `http.Client` carries NO `Timeout` — the client cap covers whole-body reads and would clamp transfers.
2. **Spinner**: Show spinner during API calls, stop before handling result
3. **Debug output**: `cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "label:", data)`
4. **Dual mode**: Work with flags (non-interactive) AND prompts (interactive) — no partial wizard
5. **Output separation**: Data → `ioStreams.Out`, prompts/warnings/debug → `ioStreams.ErrOut`

### Destructive actions MUST:

- Show warning styling (red bold) before confirmation
- Require `prompter.Confirm()` — return nil on cancel or Esc
- In agent mode (`f.AgentMode()`): require `--yes` flag, never auto-confirm

### Interactive commands MUST:

- **Show the hint bar at the bottom of every direct `Prompter.Select`** — pass `tui.WithShowHints(true)` to render `↑/↓ navigate · type to filter · enter select · esc back · ctrl+c exit` below the choices. Same for `MultiSelect` via the equivalent option. Wizard step Loaders are exempt — the wizard composite already renders its own hint bar; double-rendering is a bug.
- **Treat Ctrl+C as a hard exit, Esc as a soft back** — never show a confirmation dialog on either. Unix users expect Ctrl+C to be terminal; an "Exit?" prompt is friction, and confirmation dialogs themselves can be cancelled which makes the design contradictory. Use `cmdutil.IsPromptInterrupt(err)` for Ctrl+C and `cmdutil.IsPromptBack(err)` for Esc when the two need different handling (e.g. in a "Back to list / Exit" gate, Esc returns to the list while Ctrl+C exits the whole loop). Both are cleanly distinguishable via `cmdutil.IsPromptCancel(err)` if a flow doesn't care which key triggered it.
- **Use `cmdutil.IsPromptCancel(err)`** — never bare-`return nil` on prompter errors; distinguish clean Ctrl+C / Esc from real I/O failures and propagate the latter.

### Pricing — get this wrong and users get billed wrong:

- Instance `price_per_hour` from the API (instances AND instance-types endpoints) is the **TOTAL** hourly price of the instance. Never multiply by GPU/vCPU count. Verified live on staging 2026-08-09 (`temp/docs/c1-ondemand-instance.json`; review C1).
- Burn rate = plain sum of instance `price_per_hour` totals (+ volume `base_hourly_cost`).
- A per-unit price shown to the user is total **divided** by units (GPU count or vCPU count) — division only, and only for display.
- Volume hourly: `cmdutil.VolumeHourlyPrice(monthlyPerGB, sizeGB)` = `ceil(monthlyPerGB * sizeGB / HoursInMonth * 10000) / 10000` — the only sanctioned formula (MCP and all CLI surfaces use it).
- Volume monthly: `cmdutil.VolumeMonthlyPrice(monthlyPerGB, sizeGB)`; hourly→monthly estimates use `cmdutil.HoursInMonth` (730 = 365*24/12, matching the web frontend).

### Credentials

- AWS-style INI at `~/.verda/credentials` with `verda_` prefixed keys
- Profile support via `[profile_name]` sections
- `f.VerdaClient()` handles resolution — returns clear error if not authenticated

## Before Editing Any Command

1. Read the **nearest** `CLAUDE.md` in the command directory (e.g. `cmd/vm/CLAUDE.md`) — domain knowledge, gotchas, edge cases
2. Read the **nearest** `README.md` in the command directory — usage examples, flags, architecture
3. Read `.ai/skills/new-command.md` for the full checklist when adding/modifying commands
4. If touching pricing, auth, or agent-mode: plan first, don't code immediately

Per-command docs are auto-maintained by `/update-command-knowledge` skill.
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
make test                     # Must pass (go test -race)
make lint                     # Must pass (golangci-lint; also run by pre-commit hooks)
```

**Never** report work as complete with lint failures outstanding. Fix them before the "done" message; don't defer to the pre-commit hook. See the "Go House Style" section above for the patterns that prevent the common hits.

If you modified a command, also verify:
- `./bin/verda <command> --help` renders correctly
- Interactive mode works (prompts appear)
- Non-interactive mode works (flags only, no prompts)
- `--agent -o json` mode works (structured output, no TUI)
- `--debug` shows request/response payloads

### NEVER run the binary against the real config dir

Any manual, scripted, or pty-driven run of `./bin/verda` MUST set `VERDA_HOME` to a
throwaway directory:

```bash
VERDA_HOME=$(mktemp -d) ./bin/verda <command>   # or: make run.sandbox ARGS="<command>"
```

`VERDA_HOME` (see `options.VerdaDir`) redirects the whole config dir — credentials *and*
`config.yaml`. `VERDA_SHARED_CREDENTIALS_FILE` covers only the credentials file, so
`auth use`, `settings`, and `EnsureVerdaDir` still hit the real `~/.verda`. Use
`VERDA_HOME`.

This is not hypothetical: driving the `auth login` wizard to completion to verify a TUI
fix overwrote a developer's real `~/.verda/credentials` with test values.
`auth login` replaces an existing profile with no warning — the documented re-auth
behavior — and **a client secret cannot be read back from the API, so a clobber is
unrecoverable**. Assume any command may write to the config dir, not just the obviously
auth-shaped ones.

The repo's own suites already do this — copy them, don't hand-roll a harness:
`tests/contract/main_test.go` (`cliEnv` strips every inherited `VERDA_*`, then sets
`VERDA_HOME=t.TempDir()`) and `options/registry_credentials_test.go:168`.

## Other Agents

This repo targets Claude Code and OpenAI Codex. Claude auto-loads this file; Codex auto-loads `AGENTS.md` (execution contract). A `.cursor/rules/main.mdc` pointer exists for Cursor users but is not a primary target — if Cursor drops out of the stack, delete it rather than letting it drift.
