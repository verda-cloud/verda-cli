# MCP Server Knowledge

## Quick Reference
- Command: `verda mcp serve` (stdio) — registered in `mcp.go`
- Client: lazy `clientFunc` resolved on first tool call, cached via `sync.Once` (both value and error are latched)
- Handlers never return Go errors for expected failures: they return `mcp.NewToolResultError(...)` (isError=true) so the agent sees the message
- Argument contract errors use `toolErrorResult` → JSON envelope `{"error": {code, message, details}}` mirroring `docs/agent-errors.md`

## Domain-Specific Rules

### Concurrency
- mcp-go dispatches tool calls on a worker pool — every handler may run concurrently
- ALL lazy/shared state goes through `Server.clientOnce` (`server.go`). Do not add check-then-set fields to `Server`
- Guard: `TestLazyClientInitConcurrent` in `lazy_init_test.go` fails under `-race` without it

### Confirm gates (hard requirement, mirrors `--yes`)
- Billing: `create_vm`, `create_volume` — `confirm: true` always required (schema marks it Required, but mcp-go does not enforce schemas — the handler gate is the enforcement)
- Destructive `vm_action`: `shutdown`, `force_shutdown`, `hibernate`, `delete` — gated via the `vmActions` table's `destructive` flag
- The gate runs BEFORE `verdaClient()`/API calls; a refused call must have zero side effects
- Agent-facing codes: `CONFIRMATION_REQUIRED`, `VALIDATION_ERROR`, `MISSING_REQUIRED_FLAGS` — same names as the CLI `--agent` contract

### Action honesty
- Never report `"completed"` without observing it: default is `"accepted"`; `wait: true` polls via `cmdutil.PollInstanceStatus` (5 min timeout, nil writer — no TUI spinner over MCP)
- `vmActions.expectStatus` and the destructive set mirror `cmd/vm/action.go` — keep both in sync
- `cmdutil.PollInstanceStatus` reports GO errors on terminal failure statuses (`error`, `notfound`); `PollVolumeStatus` stops immediately on failed volume statuses (`VolumeFailedStatuses` in `cmd/util/status_messages.go`) instead of burning the timeout

### Strict args
- mcp-go v0.47 performs NO schema validation or coercion for arguments — `GetArguments()` returns the raw map; handlers must reject wrong types themselves
- Use the strict helpers in `server.go`; never write `v, _ := m["x"].(string)` (silent zero values were review NEW-6)
- JSON numbers arrive as `float64`; `int` is accepted for direct-unit-test callers; strings are rejected
- `estimate_cost` prices storage via the API's `/volume-types` catalog; a `storage_type` absent from the catalog is a `VALIDATION_ERROR` listing `cmdutil.ValidVolumeTypeNames` — never $0 (mirrors `cost/estimate.go`)

## Gotchas
- `Server.getClient` closure in `mcp.go` mutates shared `Options` (`opts.Complete()`); the Once also serializes that
- `NewServer(nil)` is used by tests for registration-only checks — handlers would nil-deref on use; always pass a real client when testing handlers (`tools_test.go` shows the `mockapi` pattern)
- `create_vm`'s `wait` default is `true` (local `pollInstance`, 3s interval); `vm_action`'s `wait` default is `false`
- `add_ssh_key` is additive (not destructive/billing) — intentionally NOT gated
- No MCP tools exist for registry/startup-script mutations; if added, they need the same gate treatment

## Relationships
- Imports `cmdutil` for `PollInstanceStatus`, `WaitOptions`, `UniqueVolumeIDs`, `VolumeHourlyPrice`, `HoursInMonth`, `ValidVolumeTypeNames`
- Tests reuse `tests/contract/mockapi` (in-process mock API, covers oauth + instances + volumes + volume-types + ssh-keys + availability + balance)
- mcp-go: `github.com/mark3labs/mcp-go@v0.47` — `AddTool` + `ToolHandlerFunc`; handler signature `(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)`
