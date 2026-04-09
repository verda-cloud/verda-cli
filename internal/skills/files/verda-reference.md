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
| "status", "overview", "dashboard", "summary" | `status` (alias: `dash`) |
| "what's available", "stock", "capacity" | `availability` |
| "instance types", "GPU types", "CPU types", "specs", "flavors" | `instance-types` |
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

## Discovery

| Command | Key Flags | Output Fields |
|---------|-----------|---------------|
| `verda locations -o json` | — | `code`, `city`, `country` |
| `verda instance-types -o json` | `--gpu`, `--cpu`, `--spot` | `name`, `price_per_hour`, `spot_price`, `gpu.number_of_gpus`, `gpu_memory.size_in_gigabytes`, `memory.size_in_gigabytes` |
| `verda availability -o json` | `--type`, `--location`, `--spot` | `location_code`, `available` |
| `verda images -o json` | `--type` (NOT `--instance-type`) | `slug` (use in --os), `name`, `category` |

## VM Create — Required Flags (`--agent` mode)

| Flag | Where to Get Value |
|------|-------------------|
| `--kind` | `gpu` or `cpu` — user intent |
| `--instance-type` | `instance-types -o json` → `name` |
| `--os` | `images --type <t> -o json` → `slug` |
| `--hostname` | User-provided or auto-generate |

**Optional flags:** `--location` (default FIN-01), `--ssh-key` (repeatable), `--is-spot`, `--os-volume-size` (default 50), `--storage-size`, `--storage-type` (NVMe/HDD), `--startup-script`, `--contract` (PAY_AS_YOU_GO/SPOT/LONG_TERM), `--from` (template), `--wait`, `--wait-timeout` (use 2m)

## VM Lifecycle

| Command | Key Flags |
|---------|-----------|
| `verda vm list -o json` | `--status` (running, offline, provisioning). Fields: `id`, `hostname`, `status`, `instance_type`, `location`, `ip`, `price_per_hour` |
| `verda vm describe <id> -o json` | — |
| `verda vm start <id> --wait` | `--yes` in agent mode |
| `verda vm shutdown <id> --wait` | `--yes` in agent mode. Alias: `stop` |
| `verda vm hibernate <id> --wait` | `--yes` in agent mode |
| `verda vm delete <id> --wait` | `--yes` **required** in agent mode. Alias: `rm` |

## Status & Cost

| Command | Output Fields |
|---------|---------------|
| `verda status -o json` | `instances` (total, running, offline, spot), `volumes` (total, attached, detached, total_size_gb), `financials` (burn_rate_hourly, balance, runway_days), `locations[]` |
| `verda cost balance -o json` | `amount`, `currency` |
| `verda cost estimate -o json` | `total.hourly`, `instance.hourly`, `os_volume.hourly`. Flags: `--type`, `--os-volume`, `--storage`, `--spot` |
| `verda cost running -o json` | `instances[]` (each: `hostname`, `hourly`, `daily`, `monthly`), `total.hourly` |

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
| `verda template list -o json` | Lists saved templates |
| `verda template show vm/<name> -o json` | Note: `vm/` prefix required |
| `verda template delete vm/<name>` | Confirm first |
| `verda template create` | Interactive — tell user to run |
| `verda template edit <name>` | Interactive — tell user to run |

Deploy: `verda --agent vm create --from <name> --hostname <name> --wait --wait-timeout 2m -o json`
Hostname patterns: `{random}` → random words, `{location}` → location code

## Volumes

| Command | Key Flags |
|---------|-----------|
| `verda volume list -o json` | `--status` (attached, detached, ordered) |
| `verda volume describe <id> -o json` | — |
| `verda volume create -o json` | `--name`, `--size`, `--type` (NVMe/HDD), `--location` |
| `verda volume action <id>` | Actions: detach, rename, resize, clone, delete |
| `verda volume trash -o json` | Recoverable within 96 hours |

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
| location | `availability --type <t>` | `location_code` |
| image/os | `images --type <t>` | `slug` |
| ssh-key ID | `ssh-key list` | `id` |
| startup-script ID | `startup-script list` | `id` |
| volume ID | `volume list` | `id` |
| VM ID / hostname | `vm list` | `id`, `hostname` |
| template name | `template list` | `name` |
