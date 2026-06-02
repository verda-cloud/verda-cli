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
  cmd/<domain>/               # One dir per domain (see per-command docs below)
    CLAUDE.md                 # Domain knowledge, gotchas, edge cases
    README.md                 # Usage examples, flags, architecture notes
  options/                    # Global CLI options, credentials
internal/skills/              # Embedded AI skill files (go:embed)
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
| `cmd/availability/` | — | Instance availability by location |
| `cmd/cost/` | — | Balance, running costs, estimates |
| `cmd/images/` | — | OS image listing |
| `cmd/instancetypes/` | — | Instance type catalog |
| `cmd/locations/` | — | Datacenter locations |
| `cmd/status/` | — | Status dashboard |
| `cmd/ssh/` | — | SSH into instances |
| `cmd/mcp/` | — | MCP server |
| `cmd/skills/` | — | AI skills management |
| `cmd/completion/` | — | Shell completions |

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

### Go House Style — avoid avoidable lint hits

The repo lints with `golangci-lint` via `make lint` (included in `make test`). These are the patterns the linters enforce — write them correctly the first time instead of fixing them in a second pass:

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

1. **Timeout context**: `ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)`
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

- Instance `price_per_hour` from API is **per-unit** (per-GPU or per-vCPU)
- Total = `price_per_hour * units` — use `cmdutil.InstanceTotalHourlyCost(inst)`
- Volume: `price_per_month_per_gb` — hourly = `ceil(monthly * size / 730 * 10000) / 10000`
- Never display raw API price as "total" without multiplying

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
make test                     # Must pass (tests + lint)
```

`make test` runs `golangci-lint` — **never** report work as complete with lint failures outstanding. Fix them before the "done" message; don't defer to the pre-commit hook. See the "Go House Style" section above for the patterns that prevent the common hits.

If you modified a command, also verify:
- `./bin/verda <command> --help` renders correctly
- Interactive mode works (prompts appear)
- Non-interactive mode works (flags only, no prompts)
- `--agent -o json` mode works (structured output, no TUI)
- `--debug` shows request/response payloads

## Other Agents

This repo targets Claude Code and OpenAI Codex. Claude auto-loads this file; Codex auto-loads `AGENTS.md` (execution contract). A `.cursor/rules/main.mdc` pointer exists for Cursor users but is not a primary target — if Cursor drops out of the stack, delete it rather than letting it drift.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **verda-cli** (7546 symbols, 19146 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/verda-cli/context` | Codebase overview, check index freshness |
| `gitnexus://repo/verda-cli/clusters` | All functional areas |
| `gitnexus://repo/verda-cli/processes` | All execution flows |
| `gitnexus://repo/verda-cli/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
