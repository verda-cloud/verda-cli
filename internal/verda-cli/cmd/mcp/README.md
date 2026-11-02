# verda mcp -- MCP server for AI agents (beta)

Exposes Verda Cloud operations as [MCP](https://modelcontextprotocol.io/) tools over stdio, for agents that cannot (or prefer not to) shell out to the CLI.

```json
{
  "mcpServers": {
    "verda": {
      "command": "verda",
      "args": ["mcp", "serve"]
    }
  }
}
```

Credentials are shared with the CLI — run `verda auth login` first in the same profile environment the agent process inherits. The client is created lazily on the first tool call; a credential error is *latched* for the server lifetime (fix credentials, then restart the server).

## Tools

| Tool | Mutating? | Notable arguments |
|------|-----------|-------------------|
| `list_locations` | no | — |
| `list_instance_types` | no | `gpu_only`, `cpu_only`, `spot` |
| `check_availability` | no | `location`, `instance_type`, `spot` |
| `vm_availability` | no | `location`, `instance_type`, `gpu_only`, `cpu_only`, `spot` |
| `list_images` | no | `instance_type`, `category` (unused today) |
| `list_vms` | no | `status` |
| `describe_vm` | no | `id` (required) |
| `get_balance` | no | — |
| `estimate_cost` | no | `instance_type` (required), `os_volume_gb`, `storage_gb`, `storage_type`, `spot` |
| `get_running_costs` | no | — |
| `list_ssh_keys` | no | `search` |
| `get_ssh_command` | no | `id_or_hostname` (required), `user`, `key_path` |
| `list_volumes` | no | — |
| `list_volumes_in_trash` | no | — |
| `add_ssh_key` | yes (additive) | `name`, `public_key` (both required) |
| `create_volume` | **yes, billing** — `confirm:true` required | `name`, `size_gb`, `confirm` (required); `type`, `location` |
| `create_vm` | **yes, billing** — `confirm:true` required | `instance_type`, `image`, `hostname`, `confirm` (required); `location`, `description`, `os_volume_size_gb`, `ssh_key_ids`, `startup_script_id`, `spot`, `storage_size_gb`, `storage_type`, `wait` (default true) |
| `vm_action` | **action-dependent** | `id`, `action` (required); `confirm`, `wait` |
| `list_container_deployments` | no | — |
| `describe_container_deployment` | no | `name` (required) |
| `list_container_replicas` | no | `name` (required) |
| `update_container_scaling` | **yes, billing** — `confirm:true` required | `name`, `confirm` (required); at least one of `min_replica_count`, `max_replica_count`, `concurrent_requests_per_replica`, `queue_message_ttl_seconds`, `scale_up_delay_seconds`, `scale_down_delay_seconds`, `queue_load_threshold`, `cpu_utilization_enabled`, `cpu_utilization_threshold`, `gpu_utilization_enabled`, `gpu_utilization_threshold` |
| `container_action` | **action-dependent** | `name`, `action` (required: `pause`, `resume`, `restart`, `purge_queue`); `confirm` |

The container tools are registered whether or not `VERDA_SERVERLESS_ENABLED` is set; that variable gates only the pre-GA `verda container` / `verda batchjob` CLI commands.

## Serverless containers

- **No secrets out.** `describe_container_deployment` returns env var *names* only, never values (which may be secrets), like the CLI describe card. Status and scaling are best-effort: if either call fails, the deployment is still returned with `status_error` / `scaling_error`.
- **Partial updates.** `update_container_scaling` sends only the fields the agent passes; scaling triggers it leaves out keep their current values. An explicit `min_replica_count: 0` is sent as 0 (scale to zero when idle), not treated as "unset".
- **Checked before the call.** Spec limits, and `min_replica_count` ≤ the current `max_replica_count` when only one is passed; a violation is `VALIDATION_ERROR` and nothing is sent.
- **Triggers.** Setting `*_utilization_threshold` turns that trigger on unless `*_utilization_enabled: false` is also passed. `*_utilization_enabled: true` alone reuses the stored threshold (error if there is none; a disabled trigger reads back with threshold 0). A disable is always sent, even when a read says the trigger is already off, because reads lag.
- **Result.** Reads show a scaling change after 10–20 s, so the tool returns `status: "accepted"` with `previous` (the read before the change, which may lag) and `expected` (the PATCH response: the stored options with the change). Replicas start and stop asynchronously — use `list_container_replicas`.
- **Transient 503.** Raising replica counts can fail with `API_ERROR` 503 "Failed to check quota. Try again"; retrying is safe.

Hibernate is deprecated and will be removed in a future version. It discontinues compute and retains volumes. Retained volumes continue billing.

## Confirmation contract

Tools that create billed resources (`create_vm`, `create_volume`, `update_container_scaling`) or perform destructive actions (`vm_action` with `shutdown`, `force_shutdown`, `hibernate` (deprecated), `delete`; `container_action` with `restart`, `purge_queue`) require the boolean argument `confirm: true`. This mirrors `--yes` in the CLI's `--agent` mode.

`confirm` is set by the agent and is not an approval check: unattended automation under standing instructions from its owner (for example, scale to `min_replica_count: 0` at night) passes it on the first call.

Without it, the tool fails with `isError: true` and a JSON payload following the CLI agent-error envelope (see [docs/agent-errors.md](../../../docs/agent-errors.md)):

```json
{"error": {"code": "CONFIRMATION_REQUIRED", "message": "action \"delete\" creates billing or destructive changes and requires an explicit confirm: true argument", "details": {"action": "delete"}}}
```

**Agent action:** show the user the exact target and (for creates) the `estimate_cost` result; only retry with `confirm: true` after explicit approval.

## Action semantics: `accepted` vs `completed`

`vm_action` reports `status: "accepted"` by default — the API has accepted the action, nothing more. `shutdown` of a running VM is *not* done at that point; do not tell the user billing stopped.

With `wait: true`, the tool polls the instance (up to 5 minutes) until it reaches the action's expected status (`start`→running, `shutdown`/`force_shutdown`/`hibernate`→offline) and then reports `status: "completed"` plus `instance_status`. A failed transition (instance enters `error`) is a tool error, not a success. `delete` is not polled and always returns `accepted`.

`create_vm` keeps `wait: true` as its default and blocks until the instance is `running`; on poll failure it returns the instance plus `poll_error`/`poll_timed_out` fields.

## Argument typing

mcp-go does not validate arguments against the declared schema. All handlers type-check arguments explicitly; mismatches fail with the envelope and code `VALIDATION_ERROR` (details: `field`, `reason`) instead of being silently coerced:

- numbers must be numbers — `"500"` for `os_volume_size_gb` is rejected (was: coerced to 0, silently applying the 50 GB default)
- arrays must be arrays — `ssh_key_ids` as a bare string is rejected (was: dropped, attaching *all* account SSH keys)
- enums are checked against their allowed sets — `storage_type` accepts only `NVMe`/`HDD`; `vm_action` `action` accepts only the five documented verbs; unknown `estimate_cost` `storage_type` errors listing the types from the API catalog instead of pricing storage at $0
- missing required arguments produce `MISSING_REQUIRED_FLAGS`

## Files

- `mcp.go` — `verda mcp` / `verda mcp serve` cobra wiring; lazy client creation
- `server.go` — server struct, `sync.Once` client init, tool-error envelope, strict argument helpers (`requiredString`, `optionalString`, `optionalBool`, `requiredInt`/`optionalInt`, `optionalEnum`, `optionalStringSlice`, `toolErrorResult`)
- `tools_discovery.go` — locations, instance types, availability, images
- `tools_cost.go` — balance, estimate (CLI pricing formula via `cmdutil`), running costs
- `tools_vm.go` — VM list/describe/create/action; `vmActions` table mirrors `cmd/vm/action.go`
- `tools_ssh.go` — SSH key list/add, ssh command construction
- `tools_volume.go` — volume list/create/trash
- `tools_container.go` — container deployment list/describe/replicas and `container_action`; `containerActions` table mirrors `cmd/serverless/container_actions.go`; views drop env var values
- `tools_container_scaling.go` — `update_container_scaling`: argument parsing, spec limits, request built against the current options
- `server_test.go` — strict helper + envelope tests; `lazy_init_test.go` — concurrent-init race repro (`-race`); `tools_test.go` — handler tests against `tests/contract/mockapi`
