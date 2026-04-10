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
- NEVER guess commands — consult the verda-reference skill or run `verda <cmd> --help`
- NEVER create resources without checking cost first
- NEVER delete/shutdown without explicit user confirmation
- NEVER hardcode instance types, locations, or image slugs — always discover them
- NEVER handle, ask for, or display credentials — auth is user-only via `verda auth login`

## Prerequisites

1. `which verda` — if missing: `brew install verda-cloud/tap/verda-cli`
2. `verda --agent auth show` — if exit code non-zero: tell user to run `verda auth login` (does not support -o json, do NOT display output)

## Classify the Request

| Type | Signal | Action |
|------|--------|--------|
| **Explore** | "what's available", "show me", "how much" | Discovery only. Do NOT create anything |
| **Deploy** | "create", "deploy", "spin up", "launch" | Deploy workflow below |
| **Manage** | "start", "stop", "delete", "SSH" | Find VM first, then act |
| **VM Info** | "my VMs", "instances", "what's running", "what's offline" | `verda --agent vm list -o json` (add `--status` to filter). Use `vm describe <id>` for a specific VM |
| **Cost** | "balance", "burn rate", "spending", "how much" | `verda --agent cost balance -o json` and/or `cost running -o json` |
| **Storage** | "volumes", "disks", "storage" | `verda --agent volume list -o json` |

### Explore — Use Specific Commands, Not `status`

Prefer the most specific command for the question. Do NOT use `verda status` as a catch-all.

| Question | Command |
|----------|---------|
| What's available / in stock? | `vm availability -o json` (filter: `--kind gpu\|cpu`) |
| Full catalog / specs / pricing? | `instance-types [--gpu\|--cpu] -o json` |
| My VMs / instances? | `vm list -o json` (filter: `--status running\|offline`) |
| Specific VM details? | `vm describe <id> -o json` |
| Balance / credits? | `cost balance -o json` |
| Running costs / spend? | `cost running -o json` |
| My volumes / storage? | `volume list -o json` |
| Overview / dashboard? | Combine: `vm list` + `cost balance` + `volume list` |

## Deploy Workflow

**Template shortcut:** `verda --agent template list -o json` — if user has templates, deploy with `verda --agent vm create --from <name> --hostname <name> --wait --wait-timeout 2m -o json` (skips steps 1-6).

Otherwise walk this chain. **ALWAYS** steps must run even if user specified values.

1. **Billing** *(skip if known)* — spot ("cheap", "testing") or on-demand (default)
2. **Compute** *(skip if known)* — GPU (ML/training/CUDA) or CPU (web/API/dev)
3. **Instance type** *(skip if user specified)* — `verda --agent instance-types [--gpu|--cpu] -o json`, present top 3 by price
4. **ALWAYS: Availability** — `verda --agent vm availability --type <type> [--spot] -o json`. Location depends on availability, NOT the reverse
5. **ALWAYS: Images** — `verda --agent images --type <type> -o json`. Use `image_type` field for `--os` flag. **NEVER guess** — they vary by instance type
6. **ALWAYS: SSH keys** — `verda --agent ssh-key list -o json`. If user named a key, find its ID
7. **ALWAYS: Cost** — `verda --agent cost balance -o json` + `verda --agent cost estimate --type <type> --os-volume 50 -o json`. Warn if runway < 24h
8. **Confirm** — show summary, wait for "yes"
9. **Create:**
   ```bash
   verda --agent vm create \
     --kind <kind> --instance-type <type> --location <loc> \
     --os <image> --hostname <name> --ssh-key <id> \
     [--is-spot] [--os-volume-size 50] --wait --wait-timeout 2m -o json
   ```
10. **Verify** — `verda --agent vm describe <id> -o json`. Tell user: `verda ssh <hostname>` (do NOT run it)

## Error Recovery

| Error Code | Action |
|------------|--------|
| `AUTH_ERROR` | Tell user: `verda auth login` |
| `INSUFFICIENT_BALANCE` | Show balance, suggest spot or smaller instance |
| `NOT_FOUND` | Re-fetch resource list, verify ID |
| `MISSING_REQUIRED_FLAGS` | Read `details.missing`, provide values, retry |
| `CONFIRMATION_REQUIRED` | Confirm with user, retry with `--yes` |
| `VALIDATION_ERROR` | Read `details.field` + `details.reason`, fix and retry |

## Presenting Results

Pick the format that fits the data:
- **Multiple items to compare** (instance types, pricing) → markdown table, keep columns minimal (4-6 max)
- **Single item** (one VM, one template) → short summary paragraph or key-value list
- **Dashboard / overview** → summary paragraph with key numbers highlighted
- **Never** dump raw JSON to the user

## Asking Good Questions

When request is vague ("I need a GPU"):
1. **Workload**: training, inference, fine-tuning? → determines GPU size
2. **Model size**: parameter count → VRAM (7B≈16GB, 13B≈24GB, 70B≈80GB+)
3. **Budget**: hourly budget constraint?

Ask ONE question at a time.
