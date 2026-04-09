---
name: verda-cloud
description: Use when the user mentions Verda Cloud, GPU/CPU VMs, cloud instances, deploying servers, ML training infrastructure, cloud costs/billing, SSH into remote machines, or verda CLI commands.
---

# Verda Cloud

## MANDATORY — Read Before Every Command

**Every `verda` command MUST include these flags:**
- `--agent` — non-interactive mode, returns structured JSON errors
- `-o json` — structured output (NEVER scrape human-readable tables)

**Example:** `verda --agent instance-types --gpu -o json`

**NEVER do these:**
- NEVER run `verda` without `--agent -o json` (except `verda ssh` which is interactive)
- NEVER guess commands — consult the verda-commands skill or run `verda <cmd> --help`
- NEVER create resources without checking cost first
- NEVER delete/shutdown without explicit user confirmation
- NEVER hardcode instance types, locations, or image slugs — always discover them

## Prerequisites

Before any verda commands, run these two checks:
1. `which verda` — if missing: `brew install verda-cloud/tap/verda-cli`
2. `verda --agent auth show -o json` — if error: tell user to run `verda auth login`

## Classify the Request First

| Type | Signal | What to Do |
|------|--------|------------|
| **Explore** | "what's available", "show me", "how much", "costs" | Discovery commands only. Do NOT create anything. |
| **Deploy** | "create", "deploy", "spin up", "launch" | Full deploy workflow (see below) |
| **Manage** | "start", "stop", "delete", "SSH" | Find the VM first, then act |
| **Troubleshoot** | "not working", "can't connect", "error" | Gather state (describe, list), then diagnose |

### Explore Flow

For "what's available" / "show pricing" / "how much":
1. `verda --agent instance-types --gpu -o json` (or `--cpu` for CPU)
2. Parse the JSON, present as a table showing: name, GPU model, VRAM, RAM, price_per_hour
3. Sort by price ascending
4. **Stop. Do not create anything.**

For "what am I spending" / "running costs":
1. `verda --agent cost running -o json`
2. `verda --agent cost balance -o json`
3. Present breakdown. **Stop.**

## Deploy Decision Framework

Walk this dependency chain when creating a VM. Steps marked **ALWAYS** must run even if the user specified values. Steps marked **skip if known** can be skipped when user provided the answer.

### 1. Billing → spot or on-demand? *(skip if known)*
- "cheap", "testing", "interruptible" → suggest **spot**
- "production", "stable", "long-running" → **on-demand**
- Default: on-demand

### 2. Compute → GPU or CPU? *(skip if known)*
- ML/AI/training/inference/CUDA/rendering → **GPU**
- Web server, API, database, dev box → **CPU**

### 3. Instance type → match requirements *(skip if user specified exact type)*
- `verda --agent instance-types [--gpu|--cpu] -o json`
- Match by: VRAM (`gpu_memory.size_in_gigabytes`), GPU count, model, RAM, price
- Present top 3 sorted by price

### 4. ALWAYS: Check availability
- `verda --agent availability --type <selected> [--spot] -o json`
- **Location depends on instance-type availability, NOT the reverse**
- If not available: suggest alternatives from availability data
- Pick cheapest available location, or respect user preference

### 5. ALWAYS: Discover compatible images
- `verda --agent images --type <instance-type> -o json`
- **NEVER guess image slugs** — they vary by instance type (e.g. cuda-13.0 exists for some types but not others)
- GPU default: pick latest Ubuntu + CUDA + Docker. CPU default: plain Ubuntu

### 6. ALWAYS: Get SSH keys
- `verda --agent ssh-key list -o json`
- If none: ask user for public key, add with `verda --agent ssh-key add -o json`
- If user specified a key name: find the matching ID from the list

### 7. ALWAYS: Cost check
- Run in parallel: `verda --agent cost balance -o json` + `verda --agent cost estimate --type <type> --os-volume 50 -o json`
- Calculate runway: balance / total_hourly = hours
- Warn if < 24h runway

### 8. Confirm with user
Show summary: instance type, location, image, SSH keys, hourly cost.
Wait for explicit "yes" before creating.

### 9. Create
```bash
verda --agent vm create \
  --kind <kind> --instance-type <type> --location <loc> \
  --os <image> --hostname <name> --ssh-key <id> \
  [--is-spot] [--os-volume-size 50] --wait --wait-timeout 2m -o json
```
If timeout reached, the VM is still provisioning — show what you have and check status with step 10.

### 10. Verify
`verda --agent vm describe <id> -o json` — confirm status is running, get IP.
Tell user to connect: `verda ssh <hostname>` (interactive — do NOT run it yourself).

## Spot VM Extras

- Volume discontinue policy: recommend `keep_detached` for important data
- Add `--os-volume-on-spot-discontinue keep_detached` to create command
- Warn user: spot VMs can be interrupted at any time

## Volume Decisions

- **OS volume**: always created with VM, default 50 GiB
- **Storage volume**: optional. NVMe = fast + expensive, HDD = slow + cheap
- **Existing volumes**: can attach detached volumes (must match VM location)
  `verda --agent volume list --status detached -o json`

## Efficiency Rules

- **Parallel**: locations, instance-types, ssh-key list, cost balance are independent — run together
- **Cache**: instance-types and locations don't change mid-session — reuse previous output
- **Skip when specific**: user says "deploy 1V100.6V" → skip instance-type listing (steps 1-3). Still ALWAYS run steps 4-7 (availability, images, SSH keys, cost)

## Error Recovery

Handle `--agent` mode structured errors (JSON on stderr):

| Error Code | Action |
|------------|--------|
| `AUTH_ERROR` | Tell user: `verda auth login` |
| `INSUFFICIENT_BALANCE` | Show balance, suggest spot or smaller instance |
| `NOT_FOUND` | Re-fetch resource list, verify ID |
| `MISSING_REQUIRED_FLAGS` | Read `details.missing`, provide values, retry |
| `CONFIRMATION_REQUIRED` | Confirm with user, retry with `--yes` |
| `VALIDATION_ERROR` | Read `details.field` + `details.reason`, fix and retry |

## Asking Good Questions

When request is vague ("I need a GPU"):
1. **Workload**: training, inference, fine-tuning? → determines GPU size
2. **Model size**: parameter count → VRAM (7B≈16GB, 13B≈24GB, 70B≈80GB+)
3. **Budget**: hourly budget constraint?

Ask ONE question at a time. Don't interrogate.
