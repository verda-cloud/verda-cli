# `verda serverless`

Manage serverless container deployments (always-on endpoints) and batch-job deployments (one-shot runs) on Verda Cloud.

```
verda serverless container   # → /container-deployments (continuous; supports spot)
verda serverless batchjob    # → /job-deployments (one-shot; deadline-based; no spot)
```

## Container deployments

### Create

Interactive wizard (launches when any of `--name`/`--image`/`--compute` is missing):

```bash
verda serverless container create
```

Non-interactive:

```bash
verda serverless container create \
  --name my-endpoint \
  --image ghcr.io/ai-dock/comfyui:cpu-22.04 \
  --compute RTX4500Ada --compute-size 1
```

With private registry + env + custom scaling:

```bash
verda serverless container create \
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
- General storage at `/data` (500 GiB) and SHM at `/dev/shm` (64 MiB) are included automatically and cannot be edited today. Flags exist (`--general-storage-size`, `--shm-size`) for forward-compatibility when the API exposes them.

### Lifecycle

```bash
verda serverless container list
verda serverless container describe my-endpoint
verda serverless container pause my-endpoint          # stop serving requests
verda serverless container resume my-endpoint
verda serverless container restart my-endpoint        # destructive; requires --yes in agent mode
verda serverless container purge-queue my-endpoint    # destructive; requires --yes in agent mode
verda serverless container delete my-endpoint         # destructive; requires --yes in agent mode
```

`list`, `describe`, `delete`, `pause`, `resume`, `restart`, `purge-queue` all support:

- `-o json|yaml` for structured output
- No positional arg → interactive picker (non-agent only)
- Positional `<name>` works in agent mode

## Batch-job deployments

### Create

Interactive wizard (launches when any of `--name`/`--image`/`--compute`/`--deadline` is missing):

```bash
verda serverless batchjob create
```

Non-interactive:

```bash
verda serverless batchjob create \
  --name nightly-embed \
  --image ghcr.io/me/embedder:v1 \
  --compute RTX4500Ada --compute-size 1 \
  --deadline 30m
```

**Required flags** (agent mode): `--name`, `--image`, `--compute`, `--deadline`.

**Batch jobs cannot use spot compute.** There is no `--spot` flag; the underlying API has no `IsSpot` field for jobs. This is intentional.

**Deadline is required** and must be `> 0s`. Each queued request gets up to `--deadline` to complete; missing or zero deadline fails validation client-side and server-side.

### Other scaling flags

- `--max-replicas` (default `3`) — worker pool cap
- `--request-ttl` (default `5m`) — how long a pending request may live in the queue before the server drops it

### Lifecycle

Identical shape to container, minus `restart` (not supported by the job-deployment API):

```bash
verda serverless batchjob list
verda serverless batchjob describe nightly-embed
verda serverless batchjob pause nightly-embed
verda serverless batchjob resume nightly-embed
verda serverless batchjob purge-queue nightly-embed   # destructive
verda serverless batchjob delete nightly-embed        # destructive
```

## Agent mode

Every destructive verb (`delete`, `restart`, `purge-queue`) requires `--yes` in agent mode — otherwise the command returns `CONFIRMATION_REQUIRED` with exit code 2. Structured JSON envelopes on stderr for errors; JSON result documents on stdout for successful operations. No prompts, ever.

```bash
verda --agent serverless container create \
  --name api --image ghcr.io/org/app:v1 \
  --compute RTX4500Ada --compute-size 1 -o json

verda --agent serverless container delete api --yes -o json
```

## Environment variables

- `--env KEY=VALUE` (repeatable) — plain env var
- `--env-secret KEY=SECRET_NAME` (repeatable) — env resolved from a project secret at runtime

Env names must match `^[A-Z_][A-Z0-9_]*$` (uppercase alphanumerics + underscore, no leading digit). Lowercase or leading-digit names are rejected client-side.

## See also

- `docs/plans/2026-04-24-serverless-container-design.md` — full design: wizard flow, SDK mapping, validation rules, v1-omissions list
- `CLAUDE.md` in this directory — domain knowledge + gotchas for future Claude sessions
- `verda registry` — manage registry credentials that `--registry-creds` references
