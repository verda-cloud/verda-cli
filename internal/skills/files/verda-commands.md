---
name: verda-commands
description: Verda CLI command reference — use alongside verda-cloud skill for flag details, parameter sources, and output field mappings.
---

# Verda CLI Command Reference

All commands support `-o json` for structured output. Use `--agent` flag for non-interactive mode.
Run `verda <command> --help` for complete flag details.

## Command Name Mapping

Users say things informally. Always translate to the correct hyphenated CLI command:

| User says | CLI command |
|-----------|------------|
| "deploy", "create VM", "create instance", "spin up", "launch" | `vm create` |
| "my VMs", "my instances", "list instances", "running machines" | `vm list` |
| "VM info", "instance info", "describe instance", "show VM" | `vm describe <id>` |
| "start", "boot", "power on" | `vm start <id>` |
| "stop", "shut down", "power off" | `vm shutdown <id>` (alias: `vm stop`) |
| "hibernate", "suspend", "sleep" | `vm hibernate <id>` |
| "delete VM", "delete instance", "remove", "destroy", "terminate" | `vm delete <id>` (alias: `vm rm`) |
| "what's available", "stock", "capacity" | `availability` or `vm availability` |
| "instance types", "GPU types", "CPU types", "machine types", "specs", "flavors" | `instance-types` |
| "pricing", "plans", "how much", "cost per hour" | `instance-types` (has pricing) or `cost estimate` |
| "images", "OS", "operating system", "Ubuntu", "CUDA" | `images` (NOT `images list`) with `--type` (NOT `--instance-type`) |
| "locations", "regions", "datacenters", "where" | `locations` |
| "ssh key", "sshkey", "SSH keys", "my keys", "public key" | `ssh-key` |
| "startup script", "init script", "boot script", "user data" | `startup-script` |
| "volume", "disk", "storage", "block storage" | `volume` |
| "balance", "credits", "funds", "account" | `cost balance` |
| "running costs", "burn rate", "spending" | `cost running` |
| "estimate", "how much will it cost" | `cost estimate` |
| "connect", "SSH in", "log in", "remote access" | Tell user to run `verda ssh <hostname>` themselves (interactive — agent cannot run it) |
| "login", "authenticate", "credentials" | `auth login` (user runs manually) |

## Auth

| Command | Purpose |
|---------|---------|
| `verda auth login` | Interactive browser auth (user runs manually) |
| `verda auth show -o json` | Check current auth status |
| `verda auth use <profile>` | Switch auth profile |

## Discovery

| Command | Purpose | Key Flags | Output Fields |
|---------|---------|-----------|---------------|
| `verda locations -o json` | List datacenters | — | `code`, `city`, `country` |
| `verda instance-types -o json` | Specs + pricing | `--gpu`, `--cpu`, `--spot` | `name`, `price_per_hour`, `spot_price`, `gpu.number_of_gpus`, `gpu_memory.size_in_gigabytes`, `memory.size_in_gigabytes`, `cpu.number_of_cores` |
| `verda availability -o json` | Stock by location/type | `--type`, `--location`, `--spot` | `location_code`, `available` |
| `verda images -o json` | OS images (NOT `images list` — no subcommand) | `--type` (NOT `--instance-type`) | `slug` (use in --os), `name`, `category` |

## VM Create

**Required flags** (in `--agent` mode):

| Flag | Type | Where to Get Value |
|------|------|-------------------|
| `--kind` | `gpu` or `cpu` | User intent or instance-type prefix |
| `--instance-type` | string | `verda instance-types -o json` → `name` field |
| `--os` | string | `verda images -o json` → `slug` field |
| `--hostname` | string | User-provided or auto-generate |

**Common optional flags:**

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--location` | string | `FIN-01` | From `verda availability` |
| `--ssh-key` | string (repeatable) | — | From `verda ssh-key list` → `id` field |
| `--is-spot` | bool | false | Enables spot pricing |
| `--os-volume-size` | int (GiB) | 50 | OS disk size |
| `--storage-size` | int (GiB) | — | Additional NVMe/HDD volume |
| `--storage-type` | `NVMe` or `HDD` | `NVMe` | Storage volume type |
| `--startup-script` | string | — | From `verda startup-script list` → `id` |
| `--contract` | string | `PAY_AS_YOU_GO` | `PAY_AS_YOU_GO`, `SPOT`, `LONG_TERM` |
| `--os-volume-on-spot-discontinue` | string | — | `keep_detached`, `move_to_trash`, `delete_permanently` |
| `--wait` | bool | true | Wait for VM to be running |
| `--wait-timeout` | duration | 5m | **Use `2m` for agent mode** — default 5m is too long |

## VM Lifecycle

| Command | Purpose | Key Flags |
|---------|---------|-----------|
| `verda vm list -o json` | List VMs | `--status` (running, offline, provisioning) |
| `verda vm describe <id> -o json` | VM details + volumes | — |
| `verda vm start <id> --wait` | Start stopped VM | `--yes` in agent mode |
| `verda vm shutdown <id> --wait` | Graceful shutdown | `--yes` in agent mode |
| `verda vm hibernate <id> --wait` | Hibernate (saves state) | `--yes` in agent mode |
| `verda vm delete <id> --wait` | Delete VM + volumes | `--yes` **required** in agent mode |

Note: `shutdown` alias is `stop`. `delete` alias is `rm`.

## Cost

| Command | Purpose | Key Flags | Output Fields |
|---------|---------|-----------|---------------|
| `verda cost balance -o json` | Account balance | — | `balance`, `currency` |
| `verda cost estimate -o json` | Price estimate | `--type`, `--os-volume`, `--storage`, `--storage-type`, `--spot` | `total_hourly`, `breakdown[]` |
| `verda cost running -o json` | Running instance costs | — | Per-instance breakdown, `total_hourly` |

## SSH (Interactive — Agent Cannot Run These)

**Do NOT run these commands.** Tell the user to run them in their terminal:

```
verda ssh <hostname-or-id>                           # SSH session
verda ssh <host> -- -L 8080:localhost:8080           # Port forwarding
verda ssh <host> -- <command>                        # Remote command
```

Flags: `--user` (default: root), `--key` (identity file path)

## SSH Keys

| Command | Purpose | Key Flags |
|---------|---------|-----------|
| `verda ssh-key list -o json` | List keys | — |
| `verda ssh-key add -o json` | Add key | `--name`, `--public-key` |
| `verda ssh-key delete <id> -o json` | Remove key | confirm first |

## Startup Scripts

| Command | Purpose | Key Flags |
|---------|---------|-----------|
| `verda startup-script list -o json` | List scripts | — |
| `verda startup-script add -o json` | Add script | `--name`, `--file` or `--script` |
| `verda startup-script delete <id> -o json` | Remove script | confirm first |

## Volumes

| Command | Purpose | Key Flags |
|---------|---------|-----------|
| `verda volume list -o json` | List volumes | `--status` (attached, detached, ordered) |
| `verda volume describe <id> -o json` | Volume details | — |
| `verda volume create -o json` | Create volume | `--name`, `--size`, `--type` (NVMe/HDD), `--location` |
| `verda volume action <id>` | Manage volume | Actions: detach, rename, resize, clone, delete |
| `verda volume trash -o json` | List trashed volumes | Recoverable within 96 hours |

## Parameter Value Sources

Quick reference: where does each parameter come from?

| Parameter | Source Command | Field |
|-----------|---------------|-------|
| instance-type | `verda instance-types -o json` | `name` |
| location | `verda availability --type <t> -o json` | `location_code` |
| image/os | `verda images --type <t> -o json` | `slug` |
| ssh-key ID | `verda ssh-key list -o json` | `id` |
| startup-script ID | `verda startup-script list -o json` | `id` |
| volume ID | `verda volume list -o json` | `id` |
| VM ID | `verda vm list -o json` | `id` |
| hostname | `verda vm list -o json` | `hostname` |
