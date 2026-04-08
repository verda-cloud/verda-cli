# Command Reference

Full reference for all Verda CLI commands and flags.

## VM

| Command | Description |
|---------|-------------|
| `verda vm create` | Create a VM (interactive wizard or flags) |
| `verda vm list` | List and inspect VM instances |
| `verda vm describe` | Show detailed info about a single VM |
| `verda vm action` | Start, shutdown, hibernate, or delete a VM |
| `verda vm availability` | Show available instance types with pricing |

## SSH

```bash
# Connect by hostname
verda ssh gpu-runner

# Connect with options
verda ssh gpu-runner --user ubuntu --key ~/.ssh/id_ed25519

# Port forwarding and other ssh args
verda ssh gpu-runner -- -L 8080:localhost:8080
```

## Volume

| Command | Description |
|---------|-------------|
| `verda volume create` | Create a block storage volume |
| `verda volume list` | List volumes |
| `verda volume describe` | Show detailed info about a single volume |
| `verda volume action` | Detach, rename, resize, clone, or delete |
| `verda volume trash` | List deleted volumes (restorable within 96h) |

## Instance Types, Images, Locations & Availability

```bash
# Browse instance types with specs and pricing
verda instance-types
verda instance-types --gpu             # GPU only
verda instance-types --cpu             # CPU only
verda instance-types --spot            # spot pricing

# List all OS images
verda images
verda images --type 1V100.6V          # compatible with instance type
verda images --category ubuntu         # filter by category

# List datacenter locations
verda locations

# Check capacity
verda availability                     # full matrix
verda availability --location FIN-01   # specific location
verda availability --type 1V100.6V     # specific type
verda availability --spot              # spot only
```

## Cost & Billing

```bash
# Estimate costs before creating
verda cost estimate --type 1V100.6V --os-volume 100 --storage 500
verda cost estimate --type 1V100.6V --spot

# See what your running instances are costing you
verda cost running

# Account balance
verda cost balance
```

## SSH Keys & Startup Scripts

| Command | Description |
|---------|-------------|
| `verda ssh-key list / add / delete` | Manage SSH keys |
| `verda startup-script list / add / delete` | Manage startup scripts |

## Auth

| Command | Description |
|---------|-------------|
| `verda auth login` | Save API credentials (interactive wizard) |
| `verda auth show` | Show active profile and credentials path |
| `verda auth use PROFILE` | Switch active auth profile |

### Credential Resolution Order

Credentials are resolved from multiple sources in order of precedence:

| Priority | Source | Example |
|----------|--------|---------|
| 1 | CLI flags (highest) | `--auth.client-id=xxx` |
| 2 | Config file | `auth.client-id` in `~/.verda/config.yaml` |
| 3 | Environment variables | `VERDA_CLIENT_ID`, `VERDA_CLIENT_SECRET` |
| 4 | Credentials file | `[default]` in `~/.verda/credentials` |

> **Note:** When `--auth.profile` is passed explicitly, the credentials file
> values for that profile override env vars — but CLI flags still win.

### Environment Variables

| Variable | Description |
|----------|-------------|
| `VERDA_CLIENT_ID` | API client ID |
| `VERDA_CLIENT_SECRET` | API client secret |
| `VERDA_PROFILE` | Credentials profile name |
| `VERDA_SHARED_CREDENTIALS_FILE` | Path to credentials file |
| `VERDA_AGENT` | Enable agent mode (`1` or `true`) |
| `VERDA_HOME` | Base directory for config (default `~/.verda`) |

```bash
# Use env vars for CI/CD pipelines
export VERDA_CLIENT_ID=your-client-id
export VERDA_CLIENT_SECRET=your-client-secret
verda vm list
```

## Settings

| Command | Description |
|---------|-------------|
| `verda settings theme` | View or change the color theme |
| `verda settings theme --select` | Interactive theme picker |

Available themes: `default`, `dracula`, `catppuccin`, `catppuccin-latte`, `nord`, `tokyonight`, `github-light`, `solarized-light`

## Update

| Command | Description |
|---------|-------------|
| `verda update` | Update to the latest version |
| `verda update --version v1.0.0` | Install a specific version (upgrade or downgrade) |
| `verda update --list` | List available versions |

## Shell Completion

```bash
# Bash
source <(verda completion bash)

# Zsh (add to ~/.zshrc or run once)
verda completion zsh > "${fpath[1]}/_verda"

# Fish
verda completion fish | source
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--output, -o` | Output format: `table`, `json`, `yaml` (default: table) |
| `--agent` | Agent mode: JSON output, no prompts, structured errors |
| `--debug` | Enable debug output (API request/response details) |
| `--timeout` | HTTP request timeout (default: 30s) |
| `--base-url` | Override API base URL |
| `--config` | Path to config file (default: `~/.verda/config.yaml`) |

## Structured Output

All list and describe commands support `--output json` and `--output yaml` for scripting:

```bash
# Pipe to jq
verda vm list -o json | jq '.[].hostname'

# YAML output
verda volume describe vol-123 -o yaml

# Use in CI/CD scripts
INSTANCE_ID=$(verda vm list -o json | jq -r '.[0].id')
```

## Wait for Operations

Async commands support `--wait` to poll until completion:

```bash
verda vm create --hostname gpu-runner --wait --wait-timeout 10m
verda vm action --id abc-123 --wait
verda volume create --name data --size 500 --wait
```

## Agent Mode (Beta)

The `--agent` flag optimizes CLI behavior for AI agents and scripts:

```bash
verda --agent vm list                  # JSON output, no interactive picker
verda --agent vm create ...            # structured errors for missing flags
verda --agent vm action --id X --action shutdown --yes  # no confirmation prompt
```

See [Agent Errors](agent-errors.md) for the structured error format.
