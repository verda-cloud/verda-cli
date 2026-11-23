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
| `list_serverless_compute` | no | — |
| `list_registry_credentials` | no | — (names only) |
| `list_secrets` | no | `kind` (`env`/`file`/`all`; names only, never values) |
| `delete_secret`, `delete_file_secret`, `delete_registry_credentials` | **yes, destructive** — `confirm:true` required | `name`, `confirm` (required); `force` |
| `list_containers` | no | `status` (exact, case-insensitive; reads one status per row) |
| `describe_container` | no | `name` (required) |
| `list_container_replicas` | no | `name` (required) |
| `create_container` | **yes, billing** — `confirm:true` required | `name`, `image`, `compute`, `confirm` (required); `compute_size`, `spot`, `port`, `registry_credentials`, `healthcheck_off`, `healthcheck_path`, `healthcheck_port`, `env[]`, `env_secrets[]`, `entrypoint[]`, `cmd[]`, `secret_mounts[]`, `min_replicas`, `max_replicas`, `concurrency`, `queue_preset`, `queue_load`, `cpu_util`, `gpu_util`, `scale_up_delay_seconds`, `scale_down_delay_seconds`, `request_ttl_seconds` |
| `container_action` | **action-dependent** | `name`, `action` (`pause`/`resume`/`restart`/`purge_queue`/`delete`; required); `confirm`, `wait`, `timeout_ms` (delete) |
| `scale_container` | **yes, billing** — `confirm:true` required | `name`, `confirm` (required); `min_replicas`, `max_replicas`, `concurrency`, `queue_preset`, `queue_load`, `cpu_util`, `gpu_util`, `scale_up_delay_seconds`, `scale_down_delay_seconds`, `request_ttl_seconds`, `wait`, `wait_timeout_seconds` |
| `get_container_env` | no | `name` (required) |
| `set_container_env` | yes (in place) | `name` (required); `env[]`, `env_secrets[]` (at least one) |
| `unset_container_env` | **yes, destructive** — `confirm:true` required | `name`, `keys[]`, `confirm` (required) |
| `list_batchjobs` | no | — |
| `describe_batchjob` | no | `name` (required) |
| `create_batchjob` | **yes, billing** — `confirm:true` required | `name`, `image`, `compute`, `deadline_seconds`, `confirm` (required); `compute_size`, `port`, `registry_credentials`, `env[]`, `env_secrets[]`, `entrypoint[]`, `cmd[]`, `secret_mounts[]`, `max_replicas`, `request_ttl_seconds` |
| `batchjob_action` | **action-dependent** | `name`, `action` (`pause`/`resume`/`purge_queue`/`delete`; required); `confirm`, `timeout_ms` (delete) |

Hibernate is deprecated and will be removed in a future version. It discontinues compute and retains volumes. Retained volumes continue billing.

Serverless arguments use the CLI's vocabulary with underscores: env vars are `KEY=VALUE` / `KEY=SECRET_NAME` strings, secret mounts are `SECRET:/path`, durations are whole seconds (`*_seconds`). The create tools build their request through the same `cmd/serverless` code as `verda container create` / `verda batchjob create`, so the scratch `/data` mount, the `:latest` rejection, the queue presets (`instant`=1, `balanced`=3, `cost-saver`=6) and the defaults are identical on both surfaces.

## Confirmation contract

Tools that create billed resources (`create_vm`, `create_volume`, `create_container`, `create_batchjob`, `scale_container`) or perform destructive actions (`vm_action` with `shutdown`, `force_shutdown`, `hibernate` (deprecated), `delete`; `container_action` with `restart`, `purge_queue`, `delete`; `batchjob_action` with `purge_queue`, `delete`; `unset_container_env`; `delete_secret`, `delete_file_secret`, `delete_registry_credentials`) require the boolean argument `confirm: true`. This mirrors `--yes` in the CLI's `--agent` mode: the gated set is exactly the set of CLI verbs that require `--yes` there (`pause`, `resume` and `env set` are gated on neither surface).

Without it, the tool fails with `isError: true` and a JSON payload following the CLI agent-error envelope (see [docs/agent-errors.md](../../../docs/agent-errors.md)):

```json
{"error": {"code": "CONFIRMATION_REQUIRED", "message": "action \"delete\" creates billing or destructive changes and requires an explicit confirm: true argument", "details": {"action": "delete"}}}
```

**Agent action:** show the user the exact target and (for creates) the `estimate_cost` result; only retry with `confirm: true` after explicit approval.

## Action semantics: `accepted` vs `completed`

`vm_action` reports `status: "accepted"` by default — the API has accepted the action, nothing more. `shutdown` of a running VM is *not* done at that point; do not tell the user billing stopped.

With `wait: true`, the tool polls the instance (up to 5 minutes) until it reaches the action's expected status (`start`→running, `shutdown`/`force_shutdown`/`hibernate`→offline) and then reports `status: "completed"` plus `instance_status`. A failed transition (instance enters `error`) is a tool error, not a success. `delete` is not polled and always returns `accepted`.

`create_vm` keeps `wait: true` as its default and blocks until the instance is `running`; on poll failure it returns the instance plus `poll_error`/`poll_timed_out` fields.

### Serverless

- `container_action` reports `accepted` by default. `wait: true` polls the deployment status (up to 5 minutes) only for `pause` (→ `paused`) and `resume` (→ `healthy`) and reports `completed` plus `deployment_status`. A transient read failure during the wait is retried until the deadline; a permanent 4xx ends it with an error that states the action itself was accepted. `restart` and `purge_queue` have no observable target and always return `accepted`.
- `batchjob_action` has no `wait` (passing it is a `VALIDATION_ERROR`): the job status vocabulary has not been verified, so the tool does not promise a target it cannot observe. Use `describe_batchjob` to watch the status.
- `list_containers` with `status` keeps rows whose status could not be read, with `status_error` set and no `status`, so a filtered list is never silently incomplete.
- `delete` passes `timeout_ms` to the API, which waits server-side (default 60 s, max 300 s) before answering; the tool reports `completed` on success. With `timeout_ms: 0` the API returns at once and the tool reports `accepted`. The call is bounded by that wait plus a margin, not by `--timeout`.
- `create_container` / `create_batchjob` do not wait: they return the deployment plus its first status read (typically `initializing`). A `min_replicas: 0` deployment may never report `healthy`, so there is nothing to wait for.
- `scale_container` reports `accepted` with `previous` and `expected`; reads lag writes by 5-20 s. `wait: true` polls until reads show every field that was sent and reports `completed` plus `current`; a wait failure keeps `accepted` and adds `wait_error` (the change is not rolled back). `completed` means the settings show in reads, not that replicas are ready (`list_container_replicas`).
- `set_container_env` / `unset_container_env` return the variables as re-read after the write (`env`), not an echo of the input, plus `added`/`updated`/`removed` counts. `removed` is observed (variables before minus after), so an unknown key counts as zero. If `set_container_env` adds succeed and the update of existing names fails, the error says how many were added.
- Validation errors speak MCP: `details.field` is the argument name and the reason uses argument names and seconds (`must be between 600s and 604800s`), not CLI flags or Go durations.

## Secrets

No tool returns a secret value and no tool creates a secret or registry credentials: that would put the value in the agent transcript. `list_secrets` and `list_registry_credentials` return names and metadata; creation is `verda container secret create`, `file-secret create`, `registry-creds create` or the web console. Plain `env` values are visible to anyone who can describe the deployment, exactly as with `verda container describe -o json`; `env_secrets` shows only the secret name.

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
- `tools_serverless.go` — shared serverless plumbing: `argReader`, `serverlessArgError` (CLI flag → MCP argument names), status polling
- `tools_serverless_catalog.go` — compute resources, secrets, registry credentials (list + gated delete)
- `tools_container.go` — container list/describe/replicas/create/action; `containerActions` mirrors `cmd/serverless/container_actions.go`
- `tools_container_scale.go` — `scale_container` on the cobra-free `cmd/serverless/scaling` package
- `tools_container_env.go` — env get/set/unset keyed on the inner container via `serverless.ContainerNameOf`
- `tools_batchjob.go` — batch-job list/describe/create/action
- `server_test.go` — strict helper + envelope tests; `lazy_init_test.go` — concurrent-init race repro (`-race`); `tools_test.go` and `tools_*_test.go` — handler tests against `tests/contract/mockapi`
