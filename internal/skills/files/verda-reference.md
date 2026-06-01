---
name: verda-reference
description: Verda CLI command reference — all commands, flags, output fields, and user intent mapping. Use alongside verda-cloud skill.
---

# Verda CLI Reference

All commands: `--agent -o json` (except `verda ssh` and `verda auth show`).

## User Intent → Command

| User says | Command |
|-----------|---------|
| "deploy", "create VM", "create instance", "spin up", "launch" | `vm create` |
| "my VMs", "my instances", "list instances", "running machines" | `vm list` |
| "VM info", "instance info", "describe", "show VM" | `vm describe <id>` |
| "start", "boot", "power on" | `vm start <id>` |
| "stop", "shut down", "power off" | `vm shutdown <id>` (alias: `stop`) |
| "hibernate", "suspend", "sleep" | `vm hibernate <id>` |
| "delete VM", "delete instance", "remove", "destroy", "terminate" | `vm delete <id>` (alias: `rm`) |
| "template", "saved config", "preset", "my templates" | `template list` (alias: `tmpl`) |
| "deploy from template", "use template", "quick deploy" | `vm create --from <name>` |
| "status", "overview", "dashboard", "summary" | Prefer `vm list` + `cost balance` + `volume list`. Use `status` only if user explicitly wants a single dashboard summary |
| "what's available", "in stock", "can I get", "available right now" | `vm availability` (real-time stock + pricing by location) |
| "instance types", "GPU types", "CPU types", "specs", "flavors", "catalog" | `instance-types` (full catalog, not filtered by stock) |
| "pricing", "how much", "cost per hour" | `instance-types` or `cost estimate` |
| "images", "OS", "Ubuntu", "CUDA" | `images` (NOT `images list`) with `--type` (NOT `--instance-type`) |
| "locations", "regions", "datacenters" | `locations` |
| "ssh key", "sshkey", "my keys", "public key" | `ssh-key` |
| "startup script", "init script", "boot script" | `startup-script` |
| "volume", "disk", "storage", "block storage" | `volume` |
| "balance", "credits", "funds" | `cost balance` |
| "running costs", "burn rate", "spending" | `cost running` |
| "estimate", "how much will it cost" | `cost estimate` |
| "connect", "SSH in", "remote access" | Tell user to run `verda ssh <host>` themselves (interactive) |
| "login", "authenticate", "credentials" | `auth login` (user runs manually) |
| "bucket", "S3", "object storage", "list buckets" | `s3 ls` |
| "upload", "put file in bucket" | `s3 cp ./file s3://bucket/key` |
| "download", "get file from bucket" | `s3 cp s3://bucket/key ./file` |
| "sync", "mirror folder to/from bucket" | `s3 sync <src> <dst>` |
| "delete object", "remove from bucket" | `s3 rm s3://bucket/key --yes` |
| "make bucket", "create bucket" / "remove bucket" | `s3 mb` / `s3 rb --yes` |
| "share link", "presigned URL", "temporary link" | `s3 presign s3://bucket/key` |
| "set up S3", "configure object storage" | `s3 configure` (user runs manually — interactive) |

## Discovery

| Command | Key Flags | Output Fields |
|---------|-----------|---------------|
| `verda locations -o json` | — | `code`, `city`, `country` |
| `verda instance-types -o json` | `--gpu`, `--cpu`, `--spot` | `name`, `price_per_hour`, `spot_price`, `gpu.number_of_gpus`, `gpu_memory.size_in_gigabytes`, `memory.size_in_gigabytes` |
| `verda vm availability -o json` | `--kind` (gpu/cpu), `--type`, `--location`, `--spot`. Use `--kind gpu` NOT `--type gpu` | `location`, `instance_type`, `gpu`, `ram`, `cpu_cores`, `price_per_hour`, `spot_price` |
| `verda images -o json` | `--type` (instance type filter, NOT `--instance-type`), `--category` (e.g. ubuntu, pytorch) | `image_type` (use in --os), `name`, `category` |

## VM Create — Required Flags (`--agent` mode)

| Flag | Where to Get Value |
|------|-------------------|
| `--kind` | `gpu` or `cpu` — user intent |
| `--instance-type` | `instance-types -o json` → `name` |
| `--os` | `images --type <t> -o json` → `image_type` field |
| `--hostname` | User-provided or auto-generate |

**Optional flags:** `--location` (default FIN-01), `--ssh-key` (repeatable, takes ID), `--is-spot`, `--os-volume-size` (GiB), `--storage-size` (GiB), `--storage-type` (NVMe/HDD), `--startup-script` (ID), `--contract` (PAY_AS_YOU_GO/SPOT/LONG_TERM), `--from` (template name), `--wait`, `--wait-timeout` (use 2m)

## VM Lifecycle

| Command | Key Flags |
|---------|-----------|
| `verda vm list -o json` | `--status`, `--location`. Fields: `id`, `hostname`, `status`, `instance_type`, `location`, `ip`, `price_per_hour` |
| `verda vm describe <id> -o json` | — |
| `verda vm start <id> --wait` | `--yes` in agent mode |
| `verda vm shutdown <id> --wait` | `--yes` in agent mode. Alias: `stop` |
| `verda vm hibernate <id> --wait` | `--yes` in agent mode |
| `verda vm delete <id> --wait` | `--yes` **required**. `--with-volumes` to also delete attached volumes. Alias: `rm` |

Batch operations: `--all` with `--status` and/or `--hostname` (glob pattern) to target multiple VMs.
Example: `verda --agent vm shutdown --all --status running --yes --wait -o json`

Note: `shutdown` alias is `stop`. `delete` alias is `rm`.

## Cost

| Command | Key Flags | Output Fields |
|---------|-----------|---------------|
| `verda cost balance -o json` | — | `amount`, `currency` |
| `verda cost estimate -o json` | `--type` (required), `--os-volume`, `--storage`, `--storage-type`, `--spot`, `--location` | `total.hourly`, `instance.hourly`, `os_volume.hourly` |
| `verda cost running -o json` | — | `instances[]` (each: `hostname`, `hourly`, `daily`, `monthly`), `total.hourly` |

## Status (Low Priority)

Prefer specific commands (`vm list`, `cost balance`, `volume list`) over `status`. Only use `status` when the user explicitly asks for a dashboard summary.

| Command | Key Flags | Output Fields |
|---------|-----------|---------------|
| `verda status -o json` | — | `instances` (total, running, offline, spot), `volumes` (total, attached, detached, total_size_gb), `financials` (burn_rate_hourly, balance, runway_days), `locations[]` |

## SSH (Interactive — Do NOT Run)

Tell user to run in their terminal:
- `verda ssh <hostname>` — SSH session
- `verda ssh <host> -- -L 8080:localhost:8080` — port forwarding

## SSH Keys & Startup Scripts

| Command | Key Flags |
|---------|-----------|
| `verda ssh-key list -o json` | — |
| `verda ssh-key add -o json` | `--name`, `--public-key` |
| `verda ssh-key delete <id> -o json` | confirm first |
| `verda startup-script list -o json` | — |
| `verda startup-script add -o json` | `--name`, `--file` or `--script` |
| `verda startup-script delete <id> -o json` | confirm first |

## Templates (alias: `tmpl`)

| Command | Notes |
|---------|-------|
| `verda template list -o json` | `--type` to filter (e.g. `--type vm`). Fields: `resource`, `name`, `description` |
| `verda template show vm/<name> -o json` | Fields: `InstanceType`, `Location`, `Image`, `SSHKeys[]`, `HostnamePattern`, `Description`. Note: `vm/` prefix required |
| `verda template delete vm/<name>` | Confirm first |
| `verda template create` | Interactive — tell user to run |
| `verda template edit <name>` | Interactive field editor — tell user to run |

Deploy from template (flags override template values):
```bash
verda --agent vm create --from <name> --hostname <name> --wait --wait-timeout 2m -o json
verda --agent vm create --from <name> --location FIN-03 -o json   # override location
```
Hostname patterns: `{random}` → random words, `{location}` → location code

## Volumes

| Command | Key Flags |
|---------|-----------|
| `verda volume list -o json` | `--status` (attached, detached, ordered) |
| `verda volume describe <id> -o json` | — |
| `verda volume create -o json` | `--name`, `--size`, `--type` (NVMe/HDD), `--location` |
| `verda volume action <id>` | Actions: detach, rename, resize, clone, delete |
| `verda volume trash -o json` | Recoverable within 96 hours |

## Object Storage (S3)

Separate credentials from the main API (keys prefixed `verda_s3_`). Set up with
`verda s3 configure` (interactive — user runs it). Check status first:

| Command | Key Flags | Output Fields |
|---------|-----------|---------------|
| `verda s3 show` | `--profile` | Text key:value (NOT JSON): `s3_configured: false` only when unset; otherwise `access_key_loaded`, `secret_key_loaded`, `endpoint`, `region`. Configured ⇔ `access_key_loaded: true` |
| `verda s3 ls -o json` | — (lists buckets) | `buckets[]`: `name`, `created_at` |
| `verda s3 ls s3://bucket[/prefix] -o json` | `--recursive`, `--human-readable`, `--summarize` | `objects[]`: `key`, `size`, `modified`; `common_prefixes[]` |
| `verda s3 cp <src> <dst> -o json` | `--recursive`, `--include`, `--exclude`, `--content-type`, `--part-size`, `--concurrency`, `--no-resume`, `--dryrun` | `transfers[]`: `source`, `destination`, `bytes`, `status`; `summary` |
| `verda s3 mv <src> <dst> -o json` | same as `cp` (minus resume flags) | same as `cp` (`status: "moved"`) |
| `verda s3 rm s3://bucket/key -o json` | `--recursive`, `--include`, `--exclude`, `--dryrun`, **`--yes`** | `deleted[]`, `errors[]`, `dryrun` |
| `verda s3 sync <src> <dst> -o json` | `--delete`, `--exact-timestamps`, `--include`, `--exclude`, `--dryrun` | `transfers[]`, `deleted[]`, `summary` |
| `verda s3 mb s3://bucket -o json` | — | `bucket`, `created` |
| `verda s3 rb s3://bucket -o json` | `--force` (empty first), **`--yes`** | `bucket`, `removed`, `objects_deleted` |
| `verda s3 presign s3://bucket/key -o json` | `--expires-in` (e.g. `15m`, `24h`; default `1h`) | `url`, `expires_at` (table mode prints the bare URL to stdout) |

Rules:
- **`src`/`dst`**: at least one must be an `s3://bucket/key` URI; the other may be a local path (upload/download) or another `s3://` URI (server-side copy).
- **Destructive** (`rm`, `rb`): require `--yes` in `--agent` mode, else `CONFIRMATION_REQUIRED`. `cp`/`mv`/`sync` never prompt — the verb is the commitment.
- **`--dryrun`** previews `rm`/`cp`/`mv`/`sync` (esp. recursive + `sync --delete`) with no changes — prefer it before bulk operations.
- **Part size** accepts `MiB`/`GiB` (and loose `MB`/`M`, treated as binary). Large single-file `cp` uploads/downloads are multipart, parallel, and resumable (re-run the same command); `--no-resume` forces a fresh transfer.
- **`configure`** is interactive (tell the user to run it); everything else takes `--agent -o json`.

## Spot VMs

- Add `--is-spot` and `--os-volume-on-spot-discontinue keep_detached` to create command
- Spot VMs can be interrupted — warn user

## Volume Guidance

- OS volume: always created, default 50 GiB
- Storage: optional. NVMe = fast, HDD = cheap
- Reuse: `volume list --status detached -o json` (must match VM location)

## Efficiency

- **Parallel**: instance-types, ssh-key list, cost balance — run together
- **Cache**: instance-types and locations don't change mid-session
- **Skip**: user specifies exact type → skip steps 1-3, still ALWAYS run 4-7

## Parameter Sources

| Parameter | Source | Field |
|-----------|--------|-------|
| instance-type | `instance-types` | `name` |
| location | `vm availability --type <t>` | `location` |
| image/os | `images --type <t>` | `image_type` |
| ssh-key ID | `ssh-key list` | `id` |
| startup-script ID | `startup-script list` | `id` |
| volume ID | `volume list` | `id` |
| VM ID / hostname | `vm list` | `id`, `hostname` |
| template name | `template list` | `name` |
| bucket name | `s3 ls` | `buckets[].name` |
| object key | `s3 ls s3://bucket` | `objects[].key` |
