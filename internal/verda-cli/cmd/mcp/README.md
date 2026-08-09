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

## Confirmation contract

Tools that create billed resources (`create_vm`, `create_volume`) or perform destructive actions (`vm_action` with `shutdown`, `force_shutdown`, `hibernate`, `delete`) require the boolean argument `confirm: true`. This mirrors `--yes` in the CLI's `--agent` mode.

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
- `server_test.go` — strict helper + envelope tests; `lazy_init_test.go` — concurrent-init race repro (`-race`); `tools_test.go` — handler tests against `tests/contract/mockapi`
