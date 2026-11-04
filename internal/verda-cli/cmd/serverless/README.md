# `verda container` / `verda batchjob`

Manage serverless container deployments (always-on endpoints) and batch-job deployments (one-shot runs) on Verda Cloud.

```
verda container   # → /container-deployments (continuous; supports spot)
verda batchjob    # → /job-deployments (one-shot; deadline-based; no spot)
```

`verda container` is a beta: it shows in `verda --help` as `(beta)`. `verda batchjob` is pre-release: it is registered only with `VERDA_BATCHJOB_ENABLED=1` and stays hidden from `verda --help`.

Both command trees live in the `serverless` Go package (they share wizard step factories, validators, and the API cache), but are registered as top-level commands so users type `verda container ...` rather than `verda serverless container ...`.

## Container deployments

### Create

Interactive wizard (launches when any of `--name`/`--image`/`--compute` is missing):

```bash
verda container create
```

Non-interactive:

```bash
verda container create \
  --name my-endpoint \
  --image ghcr.io/ai-dock/comfyui:cpu-22.04 \
  --compute RTX4500Ada --compute-size 1
```

With private registry + env + custom scaling:

```bash
verda container create \
  --name my-api --image ghcr.io/me/llm:v1.2 \
  --compute RTX4500Ada --compute-size 1 \
  --registry-creds my-ghcr \
  --env HF_HOME=/data/.huggingface \
  --env-secret API_TOKEN=prod-token \
  --min-replicas 1 --max-replicas 10 \
  --queue-preset cost-saver \
  --scale-down-delay 600s
```

**Required flags** (agent mode): `--name`, `--image`, `--compute`. Interactive mode launches the wizard if any are missing.

**Images must use a specific tag.** `:latest` (explicit or implicit) is rejected before the API call.

**Deployment names** are URL slugs (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, max 63 chars). They become part of `https://containers.datacrunch.io/<name>` and are **immutable** after create.

### Scaling presets

`--queue-preset` maps to a queue-load threshold written into `ScalingTriggers.QueueLoad`:

| Preset | Queue load | When to use |
|--------|-----------|-------------|
| `instant` | 1 | Scale up on any waiting request. Minimizes time in queue. |
| `balanced` (default) | 3 | Short queue wait before scaling up. Good for most APIs. |
| `cost-saver` | 6 | Fewer replicas; requests may wait longer in queue. |
| `custom` | `--queue-load <N>` | Specify a threshold yourself (1..1000). |

`--queue-load <N>` without an explicit `--queue-preset` is treated as custom.

### Other scaling flags

- `--min-replicas` (default `0`, scale-to-zero) / `--max-replicas` (default `3`)
- `--concurrency` (default `1` — set higher for LLMs, 1 for image generation)
- `--cpu-util <pct>`, `--gpu-util <pct>` — enable the corresponding trigger (blank = off)
- `--scale-up-delay`, `--scale-down-delay` (default `5m`) — hysteresis before scaling
- `--request-ttl` (default `5m`) — how long a pending request may live before the queue drops it

### Healthcheck

- `--healthcheck-off` disables probing — requests route immediately
- `--healthcheck-port` (default = exposed port)
- `--healthcheck-path` (default `/health`)

### Storage

- `--secret-mount SECRET:/path` (repeatable) — mount a project secret as a file
- Every deployment gets a `scratch` mount at `/data` automatically; the server allocates and sizes it. `/dev/shm` is provided by the runtime. There are no flags to resize either — the API does not yet accept client-provided sizes.

### Lifecycle

```bash
verda container list
verda container describe my-endpoint
verda container pause my-endpoint          # stop serving requests
verda container resume my-endpoint
verda container restart my-endpoint        # destructive; requires --yes in agent mode
verda container purge-queue my-endpoint    # destructive; requires --yes in agent mode
verda container delete my-endpoint         # destructive; requires --yes in agent mode
verda container scale my-endpoint --max-replicas 5
verda container replicas my-endpoint
```

`describe`, `delete`, `pause`, `resume`, `restart`, `purge-queue`, `scale` and `replicas` all support:

- `-o json|yaml` for structured output
- No positional arg → interactive picker, only on a terminal with table output (not in agent mode, not with `-o json|yaml`, not with a redirected stdout); otherwise `MISSING_REQUIRED_FLAGS`
- Positional `<name>` works in every mode

`describe` also shows the scaling options. If the scaling read fails, the deployment still shows, with a note (`scaling_error` in JSON).

`list --status <s>` is an exact, case-insensitive match (`--status healthy` does not list `unhealthy`).

### Scale

Change the scaling options of a deployment. Only the options you set are sent; everything else, including triggers you leave out, keeps its current value.

```bash
verda container scale                                        # interactive: pick a deployment and the settings
verda container scale my-api --min-replicas 1 --max-replicas 5
verda container scale my-api --min-replicas 0 --yes --wait   # scale to zero when idle; wait for reads
verda container scale my-api --cpu-util 70 --gpu-util off
```

| Flag | Range | Notes |
|------|-------|-------|
| `--min-replicas` | 0..1000 | Replicas above 0 run and are billed with no traffic. Must not exceed max. With `--max-replicas` too, the CLI checks before sending. Alone, the API checks against the stored max; the CLI only warns when the last read shows a lower max. |
| `--max-replicas` | 1..1000 | For zero replicas while idle, use `--min-replicas 0` or `pause`. |
| `--concurrency` | 1..5000 | |
| `--queue-preset` / `--queue-load` | 1..1000 | Same presets as create. `--queue-load` accepts fractions. |
| `--cpu-util` / `--gpu-util` | 5..100, `off` | A threshold turns the trigger on. The API rejects a GPU trigger on CPU compute. |
| `--scale-up-delay` / `--scale-down-delay` | 0s..1h | Whole seconds. |
| `--request-ttl` | 0s..24h | Whole seconds. |
| `--yes` | | Skip the confirmation. Required in agent mode and without an interactive terminal. |
| `--wait` / `--wait-timeout` | default 60s | Poll until reads show the change. |

Without option flags on a terminal, a wizard shows the current options and asks which to change. Every run shows a diff and asks for confirmation (default No); every picked setting is sent, and a row equal to the last read is marked `(same as last read; still sent)`.

Reads lag writes by about 5-20 seconds. Without `--wait`, the result has status `accepted` with `previous` (the options before the change) and `expected` (the API's answer to the PATCH). With `--wait`, status `completed` adds `current`. `completed` means the settings show in reads, not that replicas are ready. If `--wait` times out, the result keeps `accepted` with `wait_error` and the command exits non-zero; the change is not rolled back.

### Replicas

```bash
verda container replicas my-api
verda container replicas my-api -o json   # {name, count, counts_by_status, replicas}
```

Lists each replica's id, status and start time, with a count per status. Use it after `scale` to watch replicas start or stop.

## Batch-job deployments

### Create

Interactive wizard (launches when any of `--name`/`--image`/`--compute`/`--deadline` is missing):

```bash
verda batchjob create
```

Non-interactive:

```bash
verda batchjob create \
  --name nightly-embed \
  --image ghcr.io/me/embedder:v1 \
  --compute RTX4500Ada --compute-size 1 \
  --deadline 30m
```

**Required flags** (agent mode): `--name`, `--image`, `--compute`, `--deadline`.

**Batch jobs cannot use spot compute.** There is no `--spot` flag; the underlying API has no `IsSpot` field for jobs. This is intentional.

**Deadline is required** and must be between `10m` and `7d`. Each queued request gets up to `--deadline` to complete; a missing or out-of-range deadline fails validation client-side and server-side.

### Other scaling flags

- `--max-replicas` (default `3`) — worker pool cap
- `--request-ttl` (default `5m`) — how long a pending request may live in the queue before the server drops it

### Lifecycle

Identical shape to container, minus `restart` (not supported by the job-deployment API):

```bash
verda batchjob list
verda batchjob describe nightly-embed
verda batchjob pause nightly-embed
verda batchjob resume nightly-embed
verda batchjob purge-queue nightly-embed   # destructive
verda batchjob delete nightly-embed        # destructive
```

## Agent mode

Every destructive verb (`delete`, `restart`, `purge-queue`) and `scale` (a billing change) requires `--yes` in agent mode — otherwise the command returns `CONFIRMATION_REQUIRED` with exit code 2. `pause`, `resume`, `restart` and `purge-queue` report `"status": "accepted"`: the API accepted the request, and the deployment changes afterwards. Structured JSON envelopes on stderr for errors; JSON result documents on stdout for successful operations. No prompts, ever.

```bash
verda --agent container create \
  --name api --image ghcr.io/org/app:v1 \
  --compute RTX4500Ada --compute-size 1 -o json

verda --agent container delete api --yes -o json
```

## Environment variables

- `--env KEY=VALUE` (repeatable) — plain env var
- `--env-secret KEY=SECRET_NAME` (repeatable) — env resolved from a project secret at runtime

Env names must match `^[A-Z_][A-Z0-9_]*$` (uppercase alphanumerics + underscore, no leading digit). Lowercase or leading-digit names are rejected client-side.

The describe card shows env names only. `-o json|yaml` and `--agent` print plain values verbatim, as the API returns them. Put sensitive values in secrets (`--env-secret`): output shows only the secret name.

## See also

- `CLAUDE.md` in this directory — domain knowledge + gotchas for future Claude sessions
- Registry credentials and secrets are created in the Verda web console. `--registry-creds`, `--env-secret` and `--secret-mount` reference them by name. (`verda registry` manages vccr.io access, not these deployment credentials.)
