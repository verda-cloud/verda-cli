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
- Destructive `vm_action`: `shutdown`, `force_shutdown`, `hibernate` (deprecated), `delete` — gated via the `vmActions` table's `destructive` flag
- Serverless, mirroring the CLI `--yes` set exactly: `create_container`, `create_batchjob`, `scale_container`, `unset_container_env`, `delete_secret` / `delete_file_secret` / `delete_registry_credentials` always; `container_action` for `restart` / `purge_queue` / `delete`; `batchjob_action` for `purge_queue` / `delete` (`containerActions` / `batchjobActions` tables). `pause`, `resume`, `set_container_env` are NOT gated, as in the CLI
- The gate runs BEFORE `verdaClient()`/API calls; a refused call must have zero side effects. Serverless create/scale run the offline `Request()` validation before the gate so argument errors surface first; tests assert zero requests via `mock.RequestCount`
- Agent-facing codes: `CONFIRMATION_REQUIRED`, `VALIDATION_ERROR`, `MISSING_REQUIRED_FLAGS` — same names as the CLI `--agent` contract

### API timeout
- The shared `http.Client` has no `Timeout`: every handler bounds its API calls, OAuth token request included, with `s.apiContext(ctx)` (`--timeout`, set via `SetAPITimeout` in `mcp.go`); `wait` polling keeps the raw ctx and its own deadline

### Action honesty
- Never report `"completed"` without observing it: default is `"accepted"`; `wait: true` polls via `cmdutil.PollInstanceStatus` (5 min timeout, nil writer — no TUI spinner over MCP)
- `vmActions.expectStatus` and the destructive set mirror `cmd/vm/action.go` — keep both in sync
- `cmdutil.PollInstanceStatus` reports GO errors on terminal failure statuses (`error`, `notfound`); `PollVolumeStatus` stops immediately on failed volume statuses (`VolumeFailedStatuses` in `cmd/util/status_messages.go`) instead of burning the timeout
- Serverless: `container_action` polls only `pause` → `paused` and `resume` → `healthy` (`waitForStatus`, 5 min); `restart` / `purge_queue` stay `accepted`. Like `scaling.Wait`, a transient read error is "not yet" and only a permanent 4xx (`scaling.Permanent`) ends the wait. `delete` is `completed` because the API waits server-side for `timeout_ms` (bounded by `serverless.DeleteTimeout`, not `--timeout`); `timeout_ms: 0` is `accepted`. `scale_container` uses `scaling.Wait` and keeps `accepted` + `wait_error` on a failed wait (the result's `previous`/`expected` are worth more than an error string). Creates never wait
- `batchjob_action` has NO `wait`: the job status vocabulary is unverified on staging, so `wait: true` is a `VALIDATION_ERROR` and `batchjobActions` has no `expectStatus`. Once `/job-deployments/{name}/status` is confirmed to report `paused` / `healthy`, set `expectStatus` on pause/resume, drop the rejection in `handleBatchjobAction`, and re-add the `wait` schema argument

### Serverless: import, never mirror
- `create_container` / `create_batchjob` start from `serverless.NewContainerCreateOptions()` / `NewBatchjobCreateOptions()` (the CLI defaults) and build via `Request()` — the scratch mount, `:latest` rejection, preset math and SDK validation live in `cmd/serverless`; `wire_format_test.go` there protects this surface too
- `serverlessArgError` rewrites the validators' `--flag` messages and `scaling.FieldError` into `VALIDATION_ERROR` with `details.field` in argument form (`flagToArg` holds the renames; `*_seconds` for durations). Add a row there when an argument name diverges from its flag
- Env tools resolve the inner container with `serverless.ContainerNameOf` and read the grouped env GET via `serverless.FetchEnvVars`; never send the deployment name as `container_name` (production 404). `set` splits add/update with `serverless.PartitionEnvVars`
- `argReader` latches the first argument error so the many-argument handlers stay under gocyclo; `optionalIntPtr` (nil = absent) is mandatory where 0 is legal (`min_replicas`, `cpu_util` off): `optionalInt` cannot tell absent from 0

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
- No MCP tools exist for `verda registry` (vccr.io) or startup-script mutations; if added, they need the same gate treatment
- No tool creates a secret, file secret or registry credentials (the value would travel through the agent transcript) and none returns a value; `list_secrets` is names only (`TestListSecretsNamesOnly` greps the output for `"value"`)
- `list_containers` without `status` carries no status (the list endpoint has none); with `status` it reads one status per row (`filterContainersByStatus`, 8 reads in flight so they share the `--timeout` budget concurrently). A row whose read failed is KEPT with `status_error` and no `status`: silently dropping it would hand the agent an incomplete list it cannot detect
- `unset_container_env` reports `removed` as observed (vars before minus after), not the request length; `set_container_env` says so in the error when adds landed and the update failed
- `update_container` / `update_batchjob` are absent: the CLI merge depends on `cmd.Flags().Changed()`; `scale_container` and the env tools cover the common changes

## Relationships
- Imports `cmdutil` for `PollInstanceStatus`, `Poll`, `WaitOptions`, `UniqueVolumeIDs`, `VolumeHourlyPrice`, `HoursInMonth`, `ValidVolumeTypeNames`; `cmd/serverless` for the exported create builders, env helpers, `DeleteTimeout`, `ResolveQueueLoad`, `Preset*`; `cmd/serverless/scaling` for `Change`, `Wait`, `BoundWarning`, `FieldError`
- Tests reuse `tests/contract/mockapi` (in-process mock API, covers oauth + instances + volumes + volume-types + ssh-keys + availability + balance + container deployments incl. env vars + job deployments + serverless catalog); `mock.RequestCount(method, path)` is the zero-side-effects probe, `DeleteQueries()` the `timeout`/`force` wire check
- mcp-go: `github.com/mark3labs/mcp-go@v0.47` — `AddTool` + `ToolHandlerFunc`; handler signature `(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)`
